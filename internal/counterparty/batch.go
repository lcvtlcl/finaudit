package counterparty

import (
	"context"
	"strings"

	"github.com/wwpp/finaudit/internal/models"
)

// maxUniqueLookups — защита от превышения дневного лимита DaData (10 000 запросов/сутки
// на бесплатном тарифе) при обработке одной большой выписки.
const maxUniqueLookups = 50

// CollectStatuses извлекает уникальные ИНН из транзакций и запрашивает их статус в DaData.
// Ошибки по отдельным ИНН не прерывают обработку — такой контрагент просто не попадёт
// в результат (compliance.Run() пропустит его при формировании comp2-флагов).
// Если client == nil (токен DaData не задан), возвращает nil без сетевых запросов.
func CollectStatuses(ctx context.Context, client *Client, txs []models.Transaction) []models.CounterpartyCheck {
	if client == nil {
		return nil
	}

	seen := make(map[string]bool)
	var checks []models.CounterpartyCheck

	for _, tx := range txs {
		inn := strings.TrimSpace(tx.INN)
		if inn == "" || seen[inn] {
			continue
		}
		if len(seen) >= maxUniqueLookups {
			break
		}
		seen[inn] = true

		status, err := client.Lookup(ctx, inn)
		if err != nil {
			continue
		}

		checks = append(checks, models.CounterpartyCheck{
			INN:              status.INN,
			Name:             status.Name,
			State:            status.State,
			MassAddress:      status.MassAddress,
			Disqualified:     status.Disqualified,
			RegistrationDate: status.RegistrationDate,
		})
	}

	return checks
}
