// Package counterparty — проверка контрагентов по ИНН через ЕГРЮЛ/ЕГРИП (DaData).
// Данные внешние (не ИИ), используются только как факты для compliance-правил (comp2).
package counterparty

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// Status — статус контрагента по данным ЕГРЮЛ/ЕГРИП.
type Status struct {
	INN              string    `json:"inn"`
	Name             string    `json:"name"`
	State            string    `json:"state"`        // ACTIVE | LIQUIDATING | LIQUIDATED | BANKRUPT
	MassAddress      bool      `json:"mass_address"` // массовый адрес: задел под отдельный источник ФНС, сейчас не заполняется
	Disqualified     bool      `json:"disqualified"` // дисквалификация руководителя
	LiquidationRaw   string    `json:"liquidation_raw,omitempty"`
	RegistrationDate time.Time `json:"registration_date,omitempty"` // дата регистрации в ЕГРЮЛ/ЕГРИП
}

// Client — клиент DaData Suggestions API (party endpoint).
type Client struct {
	apiKey     string
	httpClient *http.Client
	baseURL    string
}

// NewClientFromEnv создаёт клиент, читая токен из переменной окружения DADATA_API_KEY.
// Если токен не задан, проверка контрагентов отключается (не блокирует остальной аудит).
func NewClientFromEnv() (*Client, error) {
	key := os.Getenv("DADATA_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("counterparty: переменная окружения DADATA_API_KEY не задана")
	}
	return &Client{
		apiKey:     key,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		baseURL:    "https://suggestions.dadata.ru/suggestions/api/4_1/rs/findById/party",
	}, nil
}

type daDataRequest struct {
	Query string `json:"query"`
}

type daDataResponse struct {
	Suggestions []struct {
		Data struct {
			State struct {
				Status           string `json:"status"`
				LiquidationDate  *int64 `json:"liquidation_date"`
				ActualityDate    int64  `json:"actuality_date"`
				RegistrationDate *int64 `json:"registration_date"`
			} `json:"state"`
			Name struct {
				FullWithOpf string `json:"full_with_opf"`
			} `json:"name"`
			Address struct {
				Data struct {
					MetroList []interface{} `json:"metro"`
				} `json:"data"`
			} `json:"address"`
			Management struct {
				Disqualified interface{} `json:"disqualified"`
			} `json:"management"`
		} `json:"data"`
	} `json:"suggestions"`
}

// Lookup запрашивает статус контрагента по ИНН в ЕГРЮЛ/ЕГРИП через DaData
// и возвращает факты (статус, дата регистрации, дисквалификация) для правил compliance.
func (c *Client) Lookup(ctx context.Context, inn string) (*Status, error) {
	inn = strings.TrimSpace(inn)
	if inn == "" {
		return nil, fmt.Errorf("counterparty: пустой ИНН")
	}

	body, err := json.Marshal(daDataRequest{Query: inn})
	if err != nil {
		return nil, fmt.Errorf("counterparty: маршалинг запроса: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("counterparty: создание запроса: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Token "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("counterparty: запрос к DaData: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("counterparty: DaData вернул статус %d", resp.StatusCode)
	}

	var parsed daDataResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("counterparty: разбор ответа DaData: %w", err)
	}
	if len(parsed.Suggestions) == 0 {
		return nil, fmt.Errorf("counterparty: контрагент с ИНН %s не найден", inn)
	}

	s := parsed.Suggestions[0].Data
	status := &Status{
		INN:   inn,
		Name:  s.Name.FullWithOpf,
		State: s.State.Status,
	}
	if s.State.LiquidationDate != nil {
		status.LiquidationRaw = fmt.Sprintf("%d", *s.State.LiquidationDate)
	}
	if s.State.RegistrationDate != nil {
		status.RegistrationDate = time.UnixMilli(*s.State.RegistrationDate)
	}
	if s.Management.Disqualified != nil {
		if v, ok := s.Management.Disqualified.(bool); ok {
			status.Disqualified = v
		}
	}

	return status, nil
}
