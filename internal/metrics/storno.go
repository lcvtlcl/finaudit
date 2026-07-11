// metrics2: учёт сторно/возвратов при подбивке TotalIncome/TotalExpense.
//
// Идея: возврат от поставщика по прошлой оплате — не новая выручка, а
// уменьшение ранее учтённого расхода. Возврат клиенту — не новый расход, а
// уменьшение ранее учтённой выручки. Детект по ключевым словам, без ИИ.
package metrics

import "strings"

var refundKeywords = []string{
	"возврат",
	"сторно",
	"коррекция ошибочно списанных",
	"ошибочно перечисленных",
	"ошибочный платеж",
	"ошибочный платёж",
}

// isRefund определяет, что платёж — возврат/сторно ранее проведённой операции.
func isRefund(purpose string) bool {
	p := strings.ToLower(purpose)
	for _, kw := range refundKeywords {
		if strings.Contains(p, kw) {
			return true
		}
	}
	return false
}
