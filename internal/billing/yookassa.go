// Package billing — приём платежей за подписку через ЮKassa (YooKassa API v3).
//
// Важно: карточные данные не проходят через наш сервис и у нас не хранятся —
// их принимает платёжный провайдер на своей стороне. Мы работаем только
// с идентификатором и статусом платежа.
package billing

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

const defaultAPIURL = "https://api.yookassa.ru/v3/payments"

// Plan — тарифный план подписки.
type Plan struct {
	Code       string // машинный код (pro)
	Title      string // то, что увидит пользователь на странице оплаты
	Amount     string // сумма строкой, как требует ЮKassa: "1490.00"
	Currency   string // RUB
	PeriodDays int    // на сколько дней продлевает подписку
}

// Plans — доступные для оплаты планы. Тариф «Старт» бесплатный и оплаты не требует,
// self-hosted продаётся по лицензии вне онлайн-оплаты.
var Plans = map[string]Plan{
	"pro": {
		Code:       "pro",
		Title:      "FinAudit Про — подписка на 1 месяц",
		Amount:     "1490.00",
		Currency:   "RUB",
		PeriodDays: 30,
	},
}

// PlanByCode возвращает план по коду.
func PlanByCode(code string) (Plan, bool) {
	p, ok := Plans[strings.TrimSpace(code)]
	return p, ok
}

// Client — клиент ЮKassa.
type Client struct {
	shopID    string
	secretKey string
	baseURL   string
	http      *http.Client
}

// NewClientFromEnv создаёт клиент из YOOKASSA_SHOP_ID и YOOKASSA_SECRET_KEY.
// Если ключи не заданы — возвращает ошибку, и приём платежей просто отключается:
// остальной сервис (аудит, прогноз, риски) работает без изменений.
func NewClientFromEnv() (*Client, error) {
	shopID := strings.TrimSpace(os.Getenv("YOOKASSA_SHOP_ID"))
	secretKey := strings.TrimSpace(os.Getenv("YOOKASSA_SECRET_KEY"))
	if shopID == "" || secretKey == "" {
		return nil, fmt.Errorf("billing: YOOKASSA_SHOP_ID / YOOKASSA_SECRET_KEY не заданы — приём платежей отключён")
	}
	return &Client{
		shopID:    shopID,
		secretKey: secretKey,
		baseURL:   defaultAPIURL,
		http:      &http.Client{Timeout: 15 * time.Second},
	}, nil
}

// Payment — платёж в терминах ЮKassa.
type Payment struct {
	ID     string `json:"id"`
	Status string `json:"status"` // pending | waiting_for_capture | succeeded | canceled
	Paid   bool   `json:"paid"`
	Amount struct {
		Value    string `json:"value"`
		Currency string `json:"currency"`
	} `json:"amount"`
	Confirmation struct {
		ConfirmationURL string `json:"confirmation_url"`
	} `json:"confirmation"`
	Metadata map[string]string `json:"metadata"`
}

// Succeeded — платёж действительно оплачен.
func (p *Payment) Succeeded() bool {
	return p != nil && p.Status == "succeeded"
}

type amountBody struct {
	Value    string `json:"value"`
	Currency string `json:"currency"`
}

type confirmationBody struct {
	Type      string `json:"type"`
	ReturnURL string `json:"return_url"`
}

type createPaymentBody struct {
	Amount       amountBody        `json:"amount"`
	Capture      bool              `json:"capture"`
	Confirmation confirmationBody  `json:"confirmation"`
	Description  string            `json:"description"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// CreatePayment создаёт платёж и возвращает объект с confirmation_url,
// на который нужно отправить пользователя для оплаты.
func (c *Client) CreatePayment(ctx context.Context, plan Plan, returnURL string, metadata map[string]string) (*Payment, error) {
	body, err := json.Marshal(createPaymentBody{
		Amount:       amountBody{Value: plan.Amount, Currency: plan.Currency},
		Capture:      true, // одностадийный платёж: списываем сразу после подтверждения
		Confirmation: confirmationBody{Type: "redirect", ReturnURL: returnURL},
		Description:  plan.Title,
		Metadata:     metadata,
	})
	if err != nil {
		return nil, fmt.Errorf("billing: маршалинг запроса: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("billing: создание запроса: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// Idempotence-Key защищает от двойного списания при ретраях/обрыве сети.
	req.Header.Set("Idempotence-Key", newIdempotenceKey())
	req.SetBasicAuth(c.shopID, c.secretKey)

	return c.do(req)
}

// GetPayment запрашивает актуальный статус платежа по его id.
//
// Это источник правды: телу вебхука мы не доверяем (его может подделать кто угодно),
// а перезапрашиваем платёж напрямую у ЮKassa.
func (c *Client) GetPayment(ctx context.Context, paymentID string) (*Payment, error) {
	paymentID = strings.TrimSpace(paymentID)
	if paymentID == "" {
		return nil, fmt.Errorf("billing: пустой id платежа")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/"+paymentID, nil)
	if err != nil {
		return nil, fmt.Errorf("billing: создание запроса: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.SetBasicAuth(c.shopID, c.secretKey)

	return c.do(req)
}

func (c *Client) do(req *http.Request) (*Payment, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("billing: запрос к ЮKassa: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("billing: ЮKassa вернула статус %d", resp.StatusCode)
	}

	var p Payment
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		return nil, fmt.Errorf("billing: разбор ответа ЮKassa: %w", err)
	}
	return &p, nil
}

// newIdempotenceKey — ключ идемпотентности для POST-запросов (UUID v4).
//
// ЮKassa принимает любое случайное значение, но канон — UUID v4.
// Смысл: если запрос повторится (обрыв сети, ретрай), ЮKassa вернёт результат
// первого запроса, а не создаст второй платёж — то есть не спишет деньги дважды.
func newIdempotenceKey() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// крайне маловероятно; запасной вариант — метка времени
		return fmt.Sprintf("finaudit-%d", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40 // версия 4
	b[8] = (b[8] & 0x3f) | 0x80 // вариант RFC 4122
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
