package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"github.com/wwpp/finaudit/internal/notify"
	"github.com/wwpp/finaudit/internal/storage/postgres"
)

// contactAPI — обратная связь с сайта: обращения падают к нам в БД.
// Внешняя почта не нужна, поэтому нет и зависимости от почтового провайдера.
type contactAPI struct {
	store   *postgres.Store
	logger  *slog.Logger
	tg      *notify.Telegram // nil, если оповещения не настроены
	limiter *rateLimiter     // защита от спама: форма открыта без авторизации
}

func newContactAPI(store *postgres.Store, logger *slog.Logger, tg *notify.Telegram) *contactAPI {
	return &contactAPI{
		store:  store,
		logger: logger,
		tg:     tg,
		// 3 обращения за 10 минут с одного адреса — человеку хватит, спамеру нет
		limiter: newRateLimiter(3, 10*time.Minute),
	}
}

// человекочитаемые названия тем для оповещения
var contactTopicTitles = map[string]string{
	"general":    "Общий вопрос",
	"selfhosted": "Self-hosted / внедрение",
	"billing":    "Оплата и возврат",
	"privacy":    "Персональные данные",
}

func (c *contactAPI) registerRoutes(r chi.Router) {
	r.Post("/api/contact", c.handleContact())
}

// допустимые темы обращения
var contactTopics = map[string]bool{
	"general":    true, // общий вопрос
	"selfhosted": true, // развёртывание в своём контуре
	"billing":    true, // оплата и возврат
	"privacy":    true, // обработка персональных данных
}

type contactRequest struct {
	Name    string `json:"name"`
	Contact string `json:"contact"`
	Topic   string `json:"topic"`
	Message string `json:"message"`
}

func (c *contactAPI) handleContact() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if c.store == nil {
			http.Error(w, "форма временно недоступна", http.StatusServiceUnavailable)
			return
		}

		if !c.limiter.allow(clientIP(r)) {
			w.Header().Set("Retry-After", "600")
			http.Error(w, "слишком много обращений, попробуйте позже", http.StatusTooManyRequests)
			return
		}

		// ограничиваем тело, чтобы не залить нам базу мегабайтами текста
		r.Body = http.MaxBytesReader(w, r.Body, 16*1024)

		var req contactRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "некорректный запрос", http.StatusBadRequest)
			return
		}

		req.Name = strings.TrimSpace(req.Name)
		req.Contact = strings.TrimSpace(req.Contact)
		req.Topic = strings.TrimSpace(req.Topic)
		req.Message = strings.TrimSpace(req.Message)

		switch {
		case utf8.RuneCountInString(req.Name) < 2 || utf8.RuneCountInString(req.Name) > 100:
			http.Error(w, "укажите имя", http.StatusBadRequest)
			return
		case utf8.RuneCountInString(req.Contact) < 3 || utf8.RuneCountInString(req.Contact) > 200:
			http.Error(w, "укажите, как с вами связаться", http.StatusBadRequest)
			return
		case !contactTopics[req.Topic]:
			http.Error(w, "выберите тему обращения", http.StatusBadRequest)
			return
		case utf8.RuneCountInString(req.Message) < 10 || utf8.RuneCountInString(req.Message) > 4000:
			http.Error(w, "опишите вопрос подробнее (от 10 символов)", http.StatusBadRequest)
			return
		}

		if err := c.store.CreateContactRequest(r.Context(), req.Name, req.Contact, req.Topic, req.Message); err != nil {
			c.logger.Error("не удалось сохранить обращение", "err", err)
			http.Error(w, "не удалось отправить обращение", http.StatusInternalServerError)
			return
		}

		// Оповещаем команду в Telegram — в фоне, чтобы пользователь не ждал.
		// Текст заявки экранируем: это ввод из интернета.
		c.tg.SendAsync(fmt.Sprintf(
			"📩 <b>Новая заявка с сайта</b>\n\n<b>Тема:</b> %s\n<b>Имя:</b> %s\n<b>Контакт:</b> %s\n\n%s",
			notify.Escape(contactTopicTitles[req.Topic]),
			notify.Escape(req.Name),
			notify.Escape(req.Contact),
			notify.Escape(req.Message),
		))

		c.logger.Info("новое обращение с сайта", "topic", req.Topic)
		writeJSON(w, map[string]string{"status": "ok"})
	}
}

// clientIP достаёт адрес клиента с учётом того, что перед нами стоит Caddy.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if first, _, ok := strings.Cut(xff, ","); ok {
			return strings.TrimSpace(first)
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
