// Package llm — клиент DeepSeek, который превращает посчитанный AuditResult
// в человеческие выводы (Summary) и рекомендации. В модель уходят только
// агрегаты, НЕ сырые транзакции — дёшево и безопасно.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wwpp/finaudit/internal/config"
	"github.com/wwpp/finaudit/internal/models"
)

const systemPrompt = `Ты — финансовый аналитик для владельца малого бизнеса. На вход ты получаешь ТОЛЬКО посчитанные метрики (JSON), сам ничего не считаешь и не выдумываешь цифры — используешь лишь то, что дано.

Задача: объяснить картину финансов простым языком, без бухгалтерского жаргона, на русском.

Особое внимание удели:
- разрезу денежного потока: операционный поток (operating_cash_flow) — здоровье основного бизнеса; инвестиционный — разовые покупки активов; финансовый — кредиты/займы;
- кассовому разрыву (кассовый_разрыв), если он есть: когда и почему баланс ушёл в минус;
- ПРОГНОЗУ разрыва вперёд (прогноз_разрыва_вперёд), если он есть — это САМОЕ ВАЖНОЕ: предупреди заранее. Назови дату, сумму нехватки и КОНКРЕТНО что сделать — какой из ожидаемых регулярных платежей (из причины) перенести или раздробить и примерно на сколько дней, чтобы разрыва не случилось;
- проблемам из чек-листа (проблемы_из_чеклиста): по каждой значимой дай точечный совет, привязанный к её сути, а не общие слова;
- если операционный поток положительный, а разрыв вызван инвестицией — подчеркни это (бизнес здоров, проблема в разовой трате).

Верни СТРОГО валидный JSON без markdown:
{"summary": "3-5 предложений: что с деньгами сейчас и главный риск (особенно прогнозный разрыв)", "recommendations": ["конкретный совет 1", "совет 2", "совет 3", "совет 4"]}
Каждая рекомендация — с числом и действием (перенести платёж X на N дней, придержать сумму к дате разрыва, завести резерв на месяц расходов, разбить крупную предоплату). Без воды и общих фраз; если есть прогнозный разрыв — первая рекомендация должна быть про то, как его избежать.`

// Client — обёртка над DeepSeek chat completions API.
type Client struct {
	http *http.Client
}

// runtimeLLMSettings — глобальные runtime-настройки LLM для всего процесса.
// Это общий (не per-user) state, позже можно вынести на уровень пользователя.
type runtimeLLMSettings struct {
	mu      sync.RWMutex
	key     string
	model   string
	baseURL string
	inited  bool
}

var (
	runtimeSettings = &runtimeLLMSettings{}
	tokensUsed      atomic.Int64
)

var availableModels = []string{
	"deepseek-v4-flash",
	"deepseek-v3-ultra",
}

func initRuntime(cfg *config.Config) {
	runtimeSettings.mu.Lock()
	defer runtimeSettings.mu.Unlock()
	if runtimeSettings.inited {
		return
	}
	runtimeSettings.key = cfg.DeepSeekAPIKey
	runtimeSettings.model = cfg.DeepSeekModel
	runtimeSettings.baseURL = strings.TrimRight(cfg.DeepSeekBaseURL, "/")
	runtimeSettings.inited = true
}

// InitRuntimeFromConfig инициализирует runtime-настройки LLM из cfg один раз.
func InitRuntimeFromConfig(cfg *config.Config) {
	initRuntime(cfg)
}

func snapshot() (key, model, baseURL string) {
	runtimeSettings.mu.RLock()
	defer runtimeSettings.mu.RUnlock()
	return runtimeSettings.key, runtimeSettings.model, runtimeSettings.baseURL
}

func AvailableModels() []string {
	out := make([]string, len(availableModels))
	copy(out, availableModels)
	return out
}

func CurrentModel() string {
	runtimeSettings.mu.RLock()
	defer runtimeSettings.mu.RUnlock()
	return runtimeSettings.model
}

func CurrentKeyMasked() string {
	runtimeSettings.mu.RLock()
	defer runtimeSettings.mu.RUnlock()
	k := strings.TrimSpace(runtimeSettings.key)
	if k == "" {
		return ""
	}
	if len(k) <= 4 {
		return "••••"
	}
	return "•••• " + k[len(k)-4:]
}

func SetModel(model string) error {
	m := strings.TrimSpace(model)
	ok := false
	for _, allowed := range availableModels {
		if m == allowed {
			ok = true
			break
		}
	}
	if !ok {
		return fmt.Errorf("неподдерживаемая модель")
	}
	runtimeSettings.mu.Lock()
	runtimeSettings.model = m
	runtimeSettings.mu.Unlock()
	return nil
}

func SetAPIKey(key string) {
	runtimeSettings.mu.Lock()
	runtimeSettings.key = strings.TrimSpace(key)
	runtimeSettings.mu.Unlock()
}

func TokensUsed() int64 {
	return tokensUsed.Load()
}

// New создаёт клиента из конфига.
func New(cfg *config.Config) *Client {
	initRuntime(cfg)
	return &Client{
		http: &http.Client{Timeout: 60 * time.Second},
	}
}

// Enrich заполняет res.Summary/res.Recommendations через глобальный ключ (демо/fallback).
func (c *Client) Enrich(ctx context.Context, res *models.AuditResult) error {
	apiKey, model, baseURL := snapshot()
	tokens, err := c.enrich(ctx, res, baseURL, apiKey, model)
	if tokens > 0 {
		tokensUsed.Add(tokens)
	}
	return err
}

// EnrichWith — как Enrich, но с явными реквизитами (персональный ключ пользователя).
// Возвращает число израсходованных токенов для пер-юзер учёта.
func (c *Client) EnrichWith(ctx context.Context, res *models.AuditResult, baseURL, apiKey, model string) (int64, error) {
	return c.enrich(ctx, res, strings.TrimRight(baseURL, "/"), apiKey, model)
}

func (c *Client) enrich(ctx context.Context, res *models.AuditResult, baseURL, apiKey, model string) (int64, error) {
	if strings.TrimSpace(apiKey) == "" {
		return 0, fmt.Errorf("ключ ИИ не задан")
	}

	metricsJSON, err := json.Marshal(compact(res))
	if err != nil {
		return 0, fmt.Errorf("сериализация метрик: %w", err)
	}

	reqBody := chatRequest{
		Model: model,
		Messages: []message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: string(metricsJSON)},
		},
		ResponseFormat: &responseFormat{Type: "json_object"},
		Stream:         false,
	}
	raw, err := json.Marshal(reqBody)
	if err != nil {
		return 0, fmt.Errorf("сериализация запроса: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return 0, fmt.Errorf("создание запроса: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("запрос к модели: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("модель вернула %d: %s", resp.StatusCode, string(body))
	}

	var cr chatResponse
	if err := json.Unmarshal(body, &cr); err != nil {
		return 0, fmt.Errorf("разбор ответа: %w", err)
	}
	if len(cr.Choices) == 0 {
		return 0, fmt.Errorf("пустой ответ модели")
	}
	var tokens int64 = 1
	if cr.Usage != nil && cr.Usage.TotalTokens > 0 {
		tokens = cr.Usage.TotalTokens
	}

	out, err := parseModelJSON(cr.Choices[0].Message.Content)
	if err != nil {
		return tokens, fmt.Errorf("разбор JSON модели: %w (контент: %s)", err, cr.Choices[0].Message.Content)
	}
	res.Summary = strings.TrimSpace(out.Summary)
	res.Recommendations = out.Recommendations
	return tokens, nil
}

// compact оставляет только то, что нужно модели для выводов (без рядов и сырья).
func compact(r *models.AuditResult) map[string]any {
	top := r.ExpenseStructure
	if len(top) > 6 {
		top = top[:6]
	}
	m := map[string]any{
		"период":               map[string]string{"с": r.Period.From.Format("02.01.2006"), "по": r.Period.To.Format("02.01.2006")},
		"остаток_на_начало":    r.OpeningBalance,
		"остаток_на_конец":     r.ClosingBalance,
		"доходы":               r.TotalIncome,
		"расходы":              r.TotalExpense,
		"чистый_поток":         r.NetCashFlow,
		"операционный_поток":   r.OperatingCashFlow,
		"инвестиционный_поток": r.InvestingCashFlow,
		"финансовый_поток":     r.FinancingCashFlow,
		"структура_расходов":   top,
		"алерты":               r.Alerts,
	}
	if r.CashGap != nil {
		m["кассовый_разрыв"] = map[string]any{
			"дата":     r.CashGap.Date.Format("02.01.2006"),
			"нехватка": r.CashGap.Shortfall,
			"причина":  r.CashGap.Reason,
		}
	}
	if r.Forecast != nil && r.Forecast.Gap != nil {
		m["прогноз_разрыва_вперёд"] = map[string]any{
			"дата":     r.Forecast.Gap.Date.Format("02.01.2006"),
			"нехватка": r.Forecast.Gap.Shortfall,
			"причина":  r.Forecast.Gap.Reason,
		}
	}
	var issues []map[string]string
	for _, c := range r.Checks {
		if c.Status != models.CheckOK {
			issues = append(issues, map[string]string{"тема": c.Title, "суть": c.Detail})
		}
	}
	if len(issues) > 0 {
		m["проблемы_из_чеклиста"] = issues
	}
	if r.TaxRegime != "" {
		m["налоговый_режим"] = r.TaxRegime
	}
	return m
}

// parseModelJSON достаёт {summary, recommendations} даже если модель обернула в ```json.
func parseModelJSON(s string) (modelOutput, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	if i := strings.Index(s, "{"); i > 0 {
		s = s[i:]
	}
	if j := strings.LastIndex(s, "}"); j >= 0 {
		s = s[:j+1]
	}
	var out modelOutput
	err := json.Unmarshal([]byte(strings.TrimSpace(s)), &out)
	return out, err
}

// --- типы запроса/ответа DeepSeek (OpenAI-совместимый формат) ---

type chatRequest struct {
	Model          string          `json:"model"`
	Messages       []message       `json:"messages"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
	Stream         bool            `json:"stream"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type responseFormat struct {
	Type string `json:"type"`
}

type chatResponse struct {
	Choices []struct {
		Message message `json:"message"`
	} `json:"choices"`
	Usage *struct {
		TotalTokens int64 `json:"total_tokens"`
	} `json:"usage,omitempty"`
}

type modelOutput struct {
	Summary         string   `json:"summary"`
	Recommendations []string `json:"recommendations"`
}
