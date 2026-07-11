package llm

import "sort"

// Provider описывает поддерживаемого провайдера ИИ (для выпадашек, оценки стоимости и баланса).
type Provider struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	BaseURL         string   `json:"-"`
	Models          []string `json:"models"`
	PricePer1MToken float64  `json:"-"` // ориентировочная цена USD за 1M токенов (для оценки расхода)
	BalanceURL      string   `json:"-"` // GET-эндпоинт баланса, если провайдер отдаёт; иначе пусто
}

// providers — реестр. Цены ориентировочные, для оценочного показа расхода в UI.
var providers = map[string]Provider{
	"deepseek": {
		ID:              "deepseek",
		Name:            "DeepSeek",
		BaseURL:         "https://api.deepseek.com",
		Models:          []string{"deepseek-v4-flash", "deepseek-chat", "deepseek-reasoner"},
		PricePer1MToken: 0.28,
		BalanceURL:      "https://api.deepseek.com/user/balance",
	},
	"openai": {
		ID:              "openai",
		Name:            "OpenAI",
		BaseURL:         "https://api.openai.com/v1",
		Models:          []string{"gpt-4o-mini", "gpt-4o"},
		PricePer1MToken: 0.60,
	},
}

// Providers возвращает список провайдеров, отсортированный по имени.
func Providers() []Provider {
	out := make([]Provider, 0, len(providers))
	for _, p := range providers {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// GetProvider возвращает провайдера по id.
func GetProvider(id string) (Provider, bool) {
	p, ok := providers[id]
	return p, ok
}

// CostUSD оценивает стоимость израсходованных токенов у провайдера.
func CostUSD(providerID string, tokens int64) float64 {
	p, ok := providers[providerID]
	if !ok {
		return 0
	}
	return float64(tokens) / 1_000_000 * p.PricePer1MToken
}
