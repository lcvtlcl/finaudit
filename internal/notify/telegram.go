// Package notify — оповещения команды в Telegram.
//
// Нужно, чтобы заявки с сайта и оплаты не лежали в базе незамеченными:
// как только приходит обращение или проходит платёж, бот пишет в рабочий чат.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// defaultAPIBase — официальный адрес Bot API.
const defaultAPIBase = "https://api.telegram.org"

// Telegram — клиент бота. Может быть nil: тогда оповещения просто выключены
// и на работу сервиса это не влияет.
type Telegram struct {
	token   string
	chatID  string
	apiBase string
	http    *http.Client
	logger  *slog.Logger
}

// NewTelegramFromEnv создаёт клиента из TELEGRAM_BOT_TOKEN и TELEGRAM_CHAT_ID.
// Если что-то из них не задано — возвращает ошибку, и оповещения отключаются.
//
// Если api.telegram.org недоступен напрямую (например, сервер в сети, где Bot API
// заблокирован, и запрос уходит в таймаут), есть два способа обойти это без правок кода:
//
//   - TELEGRAM_PROXY_URL — прокси до Telegram API.
//     Схемы http://, https://, socks5://, в т.ч. socks5://user:pass@host:port.
//   - TELEGRAM_API_BASE_URL — свой релей Bot API (например, Cloudflare Worker,
//     который проксирует запросы на api.telegram.org). Ничего ставить на сервер не нужно.
func NewTelegramFromEnv(logger *slog.Logger) (*Telegram, error) {
	token := strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN"))
	chatID := strings.TrimSpace(os.Getenv("TELEGRAM_CHAT_ID"))
	if token == "" || chatID == "" {
		return nil, fmt.Errorf("notify: TELEGRAM_BOT_TOKEN / TELEGRAM_CHAT_ID не заданы — оповещения выключены")
	}

	apiBase := strings.TrimSuffix(strings.TrimSpace(os.Getenv("TELEGRAM_API_BASE_URL")), "/")
	if apiBase == "" {
		apiBase = defaultAPIBase
	}

	httpClient := &http.Client{Timeout: 15 * time.Second}

	if raw := strings.TrimSpace(os.Getenv("TELEGRAM_PROXY_URL")); raw != "" {
		proxyURL, err := url.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("notify: некорректный TELEGRAM_PROXY_URL: %w", err)
		}
		httpClient.Transport = &http.Transport{Proxy: http.ProxyURL(proxyURL)}
		if logger != nil {
			// сам URL не логируем: в нём может быть пароль
			logger.Info("оповещения в Telegram идут через прокси", "scheme", proxyURL.Scheme)
		}
	}

	if logger != nil && apiBase != defaultAPIBase {
		logger.Info("оповещения в Telegram идут через свой релей Bot API", "api_base", apiBase)
	}

	return &Telegram{
		token:   token,
		chatID:  chatID,
		apiBase: apiBase,
		http:    httpClient,
		logger:  logger,
	}, nil
}

type sendMessageBody struct {
	ChatID             string `json:"chat_id"`
	Text               string `json:"text"`
	ParseMode          string `json:"parse_mode"`
	DisableWebPagePrev bool   `json:"disable_web_page_preview"`
}

// Send отправляет сообщение синхронно.
func (t *Telegram) Send(ctx context.Context, text string) error {
	if t == nil {
		return nil // оповещения выключены — это не ошибка
	}

	body, err := json.Marshal(sendMessageBody{
		ChatID:             t.chatID,
		Text:               text,
		ParseMode:          "HTML",
		DisableWebPagePrev: true,
	})
	if err != nil {
		return fmt.Errorf("notify: маршалинг сообщения: %w", err)
	}

	endpoint := t.apiBase + "/bot" + t.token + "/sendMessage"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("notify: создание запроса: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.http.Do(req)
	if err != nil {
		return fmt.Errorf("notify: запрос к Telegram: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("notify: Telegram вернул статус %d", resp.StatusCode)
	}
	return nil
}

// SendAsync отправляет оповещение в фоне.
//
// Важно: пользователь не должен ждать Telegram и тем более получать ошибку,
// если бот недоступен. Поэтому шлём в отдельной горутине со своим таймаутом
// и просто логируем неудачу.
func (t *Telegram) SendAsync(text string) {
	if t == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := t.Send(ctx, text); err != nil && t.logger != nil {
			t.logger.Warn("не удалось отправить оповещение в Telegram", "err", err)
		}
	}()
}

// Escape экранирует пользовательский текст для HTML-разметки Telegram,
// чтобы содержимое заявки не сломало сообщение и не подставило свою разметку.
func Escape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}
