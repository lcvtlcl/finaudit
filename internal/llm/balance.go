package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// FetchBalance пытается получить остаток баланса аккаунта провайдера по ключу.
// Best-effort: если провайдер не отдаёт баланс или запрос не удался — ok=false.
func FetchBalance(ctx context.Context, providerID, apiKey string) (amount float64, currency string, ok bool) {
	p, exists := providers[providerID]
	if !exists || p.BalanceURL == "" || strings.TrimSpace(apiKey) == "" {
		return 0, "", false
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.BalanceURL, nil)
	if err != nil {
		return 0, "", false
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, "", false
	}
	body, _ := io.ReadAll(resp.Body)

	// Формат DeepSeek /user/balance: {"balance_infos":[{"currency":"USD","total_balance":"12.34"}]}
	var br struct {
		BalanceInfos []struct {
			Currency     string `json:"currency"`
			TotalBalance string `json:"total_balance"`
		} `json:"balance_infos"`
	}
	if err := json.Unmarshal(body, &br); err != nil || len(br.BalanceInfos) == 0 {
		return 0, "", false
	}
	amt, err := strconv.ParseFloat(strings.TrimSpace(br.BalanceInfos[0].TotalBalance), 64)
	if err != nil {
		return 0, "", false
	}
	return amt, br.BalanceInfos[0].Currency, true
}
