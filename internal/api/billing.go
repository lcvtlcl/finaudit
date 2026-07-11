package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/wwpp/finaudit/internal/billing"
	"github.com/wwpp/finaudit/internal/notify"
	"github.com/wwpp/finaudit/internal/storage/postgres"
)

// billingAPI — приём оплаты подписки через ЮKassa.
//
// Схема: чекаут создаёт платёж и отдаёт ссылку на страницу оплаты ЮKassa →
// пользователь платит на стороне провайдера → ЮKassa зовёт наш вебхук →
// мы перезапрашиваем платёж у ЮKassa и, если он действительно оплачен,
// активируем подписку.
type billingAPI struct {
	store     *postgres.Store
	client    *billing.Client // nil, если ключи ЮKassa не заданы (оплата отключена)
	logger    *slog.Logger
	publicURL string           // база для return_url, напр. https://finaudit.site
	tg        *notify.Telegram // nil, если оповещения не настроены

	// Лимит на создание платежей: не больше 5 попыток в минуту на пользователя.
	// Защищает от спама платежами (мусор в кабинете ЮKassa и лишняя нагрузка).
	limiter *rateLimiter
}

func newBillingAPI(store *postgres.Store, client *billing.Client, logger *slog.Logger, publicURL string, tg *notify.Telegram) *billingAPI {
	return &billingAPI{
		store:     store,
		client:    client,
		logger:    logger,
		publicURL: strings.TrimSuffix(publicURL, "/"),
		tg:        tg,
		limiter:   newRateLimiter(5, time.Minute),
	}
}

func (b *billingAPI) registerRoutes(r chi.Router) {
	r.Post("/api/billing/checkout", b.handleCheckout())
	r.Get("/api/billing/subscription", b.handleSubscription())
	// Вебхук зовёт ЮKassa, а не браузер пользователя — авторизации по сессии здесь нет.
	r.Post("/api/billing/webhook", b.handleWebhook())
}

// enabled — оплата настроена (есть ключи и БД).
func (b *billingAPI) enabled() bool {
	return b.client != nil && b.store != nil
}

type checkoutRequest struct {
	Plan string `json:"plan"`
}

type checkoutResponse struct {
	ConfirmationURL string `json:"confirmation_url"`
	PaymentID       string `json:"payment_id"`
}

// handleCheckout создаёт платёж и возвращает ссылку на страницу оплаты.
func (b *billingAPI) handleCheckout() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !b.enabled() {
			http.Error(w, "приём платежей временно недоступен: магазин ЮKassa ожидает одобрения", http.StatusServiceUnavailable)
			return
		}

		uidStr, ok := UserIDFromContext(r.Context())
		if !ok {
			http.Error(w, "требуется вход", http.StatusUnauthorized)
			return
		}
		userID, err := strconv.ParseInt(uidStr, 10, 64)
		if err != nil {
			http.Error(w, "некорректный пользователь", http.StatusBadRequest)
			return
		}

		// Не даём спамить созданием платежей.
		if !b.limiter.allow(uidStr) {
			b.logger.Warn("billing: превышен лимит создания платежей", "user_id", uidStr)
			w.Header().Set("Retry-After", "60")
			http.Error(w, "слишком много попыток оплаты, попробуйте через минуту", http.StatusTooManyRequests)
			return
		}

		var req checkoutRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "некорректный запрос", http.StatusBadRequest)
			return
		}

		plan, ok := billing.PlanByCode(req.Plan)
		if !ok {
			http.Error(w, "неизвестный тарифный план", http.StatusBadRequest)
			return
		}

		payment, err := b.client.CreatePayment(
			r.Context(),
			plan,
			b.publicURL+"/payment.html",
			map[string]string{
				"user_id": uidStr,
				"plan":    plan.Code,
			},
		)
		if err != nil {
			b.logger.Error("billing: не удалось создать платёж", "err", err, "user_id", uidStr)
			http.Error(w, "не удалось создать платёж", http.StatusBadGateway)
			return
		}

		if err := b.store.CreatePayment(r.Context(), userID, payment.ID, plan.Code, plan.Amount, plan.Currency); err != nil {
			b.logger.Error("billing: не удалось сохранить платёж", "err", err, "payment_id", payment.ID)
			http.Error(w, "не удалось сохранить платёж", http.StatusInternalServerError)
			return
		}

		b.logger.Info("billing: платёж создан", "payment_id", payment.ID, "user_id", uidStr, "plan", plan.Code)
		writeJSON(w, checkoutResponse{
			ConfirmationURL: payment.Confirmation.ConfirmationURL,
			PaymentID:       payment.ID,
		})
	}
}

type webhookNotification struct {
	Event  string `json:"event"`
	Object struct {
		ID string `json:"id"`
	} `json:"object"`
}

// handleWebhook принимает уведомление от ЮKassa.
//
// Телу уведомления мы НЕ доверяем: его может прислать кто угодно. Берём из него
// только id платежа и перезапрашиваем платёж напрямую у ЮKassa — статус оттуда
// и есть источник правды.
func (b *billingAPI) handleWebhook() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !b.enabled() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}

		var n webhookNotification
		if err := json.NewDecoder(r.Body).Decode(&n); err != nil || n.Object.ID == "" {
			http.Error(w, "некорректное уведомление", http.StatusBadRequest)
			return
		}

		payment, err := b.client.GetPayment(r.Context(), n.Object.ID)
		if err != nil {
			b.logger.Error("billing: не удалось проверить платёж", "err", err, "payment_id", n.Object.ID)
			// 500 — ЮKassa повторит уведомление позже
			http.Error(w, "не удалось проверить платёж", http.StatusInternalServerError)
			return
		}

		switch payment.Status {
		case "succeeded":
			row, err := b.store.GetPaymentByExternalID(r.Context(), payment.ID)
			if err != nil {
				b.logger.Error("billing: платёж не найден в БД", "err", err, "payment_id", payment.ID)
				http.Error(w, "платёж не найден", http.StatusNotFound)
				return
			}
			if row.Status == "succeeded" {
				// повторное уведомление — уже обработали, отвечаем ОК
				w.WriteHeader(http.StatusOK)
				return
			}

			plan, ok := billing.PlanByCode(row.Plan)
			if !ok {
				b.logger.Error("billing: неизвестный план в платеже", "plan", row.Plan, "payment_id", payment.ID)
				http.Error(w, "неизвестный план", http.StatusBadRequest)
				return
			}

			if err := b.store.MarkPaymentSucceeded(r.Context(), payment.ID); err != nil {
				b.logger.Error("billing: не удалось отметить платёж", "err", err, "payment_id", payment.ID)
				http.Error(w, "ошибка сохранения", http.StatusInternalServerError)
				return
			}
			if err := b.store.ActivateSubscription(r.Context(), row.UserID, plan.Code, plan.PeriodDays); err != nil {
				b.logger.Error("billing: не удалось активировать подписку", "err", err, "user_id", row.UserID)
				http.Error(w, "ошибка активации", http.StatusInternalServerError)
				return
			}

			b.logger.Info("billing: подписка активирована", "user_id", row.UserID, "plan", plan.Code, "payment_id", payment.ID)

			b.tg.SendAsync(fmt.Sprintf(
				"💰 <b>Оплата прошла</b>\n\n<b>Тариф:</b> %s\n<b>Сумма:</b> %s %s\n<b>Пользователь:</b> #%d\n<b>Подписка продлена на:</b> %d дн.",
				notify.Escape(plan.Title),
				notify.Escape(payment.Amount.Value),
				notify.Escape(payment.Amount.Currency),
				row.UserID,
				plan.PeriodDays,
			))

		case "canceled":
			if err := b.store.MarkPaymentCanceled(r.Context(), payment.ID); err != nil {
				b.logger.Error("billing: не удалось отменить платёж", "err", err, "payment_id", payment.ID)
			}
		}

		w.WriteHeader(http.StatusOK)
	}
}

type subscriptionResponse struct {
	Plan      string `json:"plan"`   // free | pro
	Status    string `json:"status"` // active | inactive
	Active    bool   `json:"active"` // подписка действует прямо сейчас
	ExpiresAt string `json:"expires_at,omitempty"`
	Enabled   bool   `json:"payments_enabled"` // настроен ли приём платежей
}

// handleSubscription отдаёт текущую подписку пользователя.
func (b *billingAPI) handleSubscription() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uidStr, ok := UserIDFromContext(r.Context())
		if !ok {
			http.Error(w, "требуется вход", http.StatusUnauthorized)
			return
		}
		userID, err := strconv.ParseInt(uidStr, 10, 64)
		if err != nil {
			http.Error(w, "некорректный пользователь", http.StatusBadRequest)
			return
		}
		if b.store == nil {
			http.Error(w, "хранилище недоступно", http.StatusServiceUnavailable)
			return
		}

		sub, err := b.store.GetSubscription(r.Context(), userID)
		if err != nil {
			http.Error(w, "не удалось получить подписку", http.StatusInternalServerError)
			return
		}

		resp := subscriptionResponse{
			Plan:    sub.Plan,
			Status:  sub.Status,
			Active:  sub.Active(),
			Enabled: b.enabled(),
		}
		if sub.ExpiresAt != nil {
			resp.ExpiresAt = sub.ExpiresAt.Format("2006-01-02")
		}
		writeJSON(w, resp)
	}
}
