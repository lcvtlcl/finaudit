// Package simulate — детерминированная песочница «что-если»: генерит сценарии из кассового
// разрыва и применяет их к копии транзакций (перенос/дробление платежа). ИИ не участвует —
// исход считает тот же движок метрик, что и основной анализ.
package simulate

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/wwpp/finaudit/internal/models"
)

// Scenarios предлагает what-if сценарии по крупнейшему списанию, вызвавшему разрыв.
func Scenarios(res models.AuditResult, txs []models.Transaction) []models.Scenario {
	if res.CashGap == nil {
		return nil
	}
	target := largestGapPayment(txs, res.CashGap.Date)
	if target == nil {
		return nil
	}
	label := strings.TrimSpace(target.Purpose)
	if label == "" {
		label = strings.TrimSpace(target.Counterparty)
	}
	amt := target.Amount
	return []models.Scenario{
		{
			Label:  fmt.Sprintf("Перенести «%s» (%.0f ₽) на 7 дней позже", shorten(label), amt),
			Action: models.SimAction{Kind: "move", MatchText: label, Amount: amt, Days: 7},
		},
		{
			Label:  fmt.Sprintf("Раздробить «%s» пополам, вторую часть через 14 дней", shorten(label)),
			Action: models.SimAction{Kind: "split", MatchText: label, Amount: amt, Days: 14},
		},
	}
}

// Apply применяет действие к копии транзакций и возвращает изменённый набор (без мутации входа).
func Apply(txs []models.Transaction, a models.SimAction) []models.Transaction {
	out := make([]models.Transaction, 0, len(txs)+1)
	applied := false
	for _, tx := range txs {
		if !applied && matches(tx, a) {
			applied = true
			switch a.Kind {
			case "move":
				tx.Date = tx.Date.AddDate(0, 0, a.Days)
				out = append(out, tx)
			case "split":
				half := math.Round(tx.Amount / 2)
				t1 := tx
				t1.Amount = half
				t2 := tx
				t2.Amount = tx.Amount - half
				t2.Date = tx.Date.AddDate(0, 0, a.Days)
				out = append(out, t1, t2)
			default:
				out = append(out, tx)
			}
			continue
		}
		out = append(out, tx)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Date.Before(out[j].Date) })
	return out
}

// Summary — компактная сводка для «было/стало».
type Summary struct {
	ClosingBalance float64 `json:"closingBalance"`
	MinBalance     float64 `json:"minBalance"`
	HasGap         bool    `json:"hasGap"`
	GapDate        string  `json:"gapDate,omitempty"`
	GapShortfall   float64 `json:"gapShortfall,omitempty"`
}

// Summarize сворачивает результат аудита в сводку для сравнения.
func Summarize(res models.AuditResult) Summary {
	s := Summary{ClosingBalance: res.ClosingBalance, MinBalance: res.ClosingBalance}
	if len(res.BalanceSeries) > 0 {
		s.MinBalance = res.BalanceSeries[0].Balance
		for _, p := range res.BalanceSeries {
			if p.Balance < s.MinBalance {
				s.MinBalance = p.Balance
			}
		}
	}
	if res.CashGap != nil {
		s.HasGap = true
		s.GapDate = res.CashGap.Date.Format("02.01.2006")
		s.GapShortfall = res.CashGap.Shortfall
	}
	return s
}

func matches(tx models.Transaction, a models.SimAction) bool {
	if tx.Direction != models.Out {
		return false
	}
	if math.Abs(tx.Amount-a.Amount) > 1 {
		return false
	}
	hay := strings.ToLower(tx.Purpose + " " + tx.Counterparty)
	return strings.Contains(hay, strings.ToLower(strings.TrimSpace(a.MatchText)))
}

func largestGapPayment(txs []models.Transaction, gapDate time.Time) *models.Transaction {
	windowStart := gapDate.AddDate(0, 0, -3)
	var best *models.Transaction
	for i := range txs {
		tx := txs[i]
		if tx.Direction != models.Out || tx.Date.Before(windowStart) || tx.Date.After(gapDate) {
			continue
		}
		if best == nil || tx.Amount > best.Amount {
			t := tx
			best = &t
		}
	}
	return best
}

func shorten(s string) string {
	r := []rune(s)
	if len(r) > 40 {
		return string(r[:40]) + "…"
	}
	return s
}
