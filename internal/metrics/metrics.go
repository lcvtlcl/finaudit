// Package metrics считает финансовые метрики по нормализованным транзакциям.
// Чистый слой: вся математика в коде, без БД, сети и LLM.
// Поля Summary/Recommendations в AuditResult НЕ заполняются — это задача LLM-слоя.
package metrics

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/wwpp/finaudit/internal/models"
)

// ComputeAudit — главная точка входа: транзакции -> AuditResult.
func ComputeAudit(txs []models.Transaction) models.AuditResult {
	res := models.AuditResult{
		ExpenseStructure: []models.ExpenseCategory{},
		CashFlow:         []models.CashFlowPoint{},
		BalanceSeries:    []models.BalancePoint{},
		Alerts:           []models.Alert{},
		Recommendations:  []string{},
	}
	if len(txs) == 0 {
		return res
	}

	// Копия + сортировка по дате. В один день приход идёт раньше расхода —
	// так дневной минимум баланса = баланс на конец дня.
	sorted := make([]models.Transaction, len(txs))
	copy(sorted, txs)
	sort.SliceStable(sorted, func(i, j int) bool {
		if !sorted[i].Date.Equal(sorted[j].Date) {
			return sorted[i].Date.Before(sorted[j].Date)
		}
		return sorted[i].Direction == models.In && sorted[j].Direction == models.Out
	})

	res.Period = models.Period{From: sorted[0].Date, To: sorted[len(sorted)-1].Date}

	// Входящий остаток — первая транзакция, если она помечена как остаток.
	opening := 0.0
	body := sorted
	if isOpeningBalance(sorted[0]) {
		opening = signed(sorted[0])
		body = sorted[1:]
	}
	res.OpeningBalance = opening

	var actualCashFlow float64
	for _, tx := range body {
		s := signed(tx)
		// Реальное движение денег по счёту — не зависит от того, выручка это
		// или внутренний перевод/возврат. NetCashFlow/баланс считаются от него.
		actualCashFlow += s

		switch {
		case isInternalTransferOrLoan(tx.Purpose):
			// metrics1: не выручка и не операционный расход — только движение денег.
			res.ExcludedTransfers += tx.Amount
		case isRefund(tx.Purpose):
			// metrics2: сторно/возврат уменьшает ранее учтённую сумму по своему
			// направлению, а не увеличивает противоположную сторону.
			if tx.Direction == models.In {
				res.TotalExpense -= tx.Amount
			} else {
				res.TotalIncome -= tx.Amount
			}
			res.NetRefunds += tx.Amount
		default:
			if tx.Direction == models.In {
				res.TotalIncome += tx.Amount
			} else {
				res.TotalExpense += tx.Amount
			}
		}

		switch tx.Activity {
		case models.ActivityInvesting:
			res.InvestingCashFlow += s
		case models.ActivityFinancing:
			res.FinancingCashFlow += s
		default:
			res.OperatingCashFlow += s
		}
	}
	res.NetCashFlow = actualCashFlow
	res.ClosingBalance = opening + res.NetCashFlow

	// Инвариант разреза ДДС по видам деятельности.
	if (res.OperatingCashFlow + res.InvestingCashFlow + res.FinancingCashFlow) != res.NetCashFlow {
		res.OperatingCashFlow = res.NetCashFlow - res.InvestingCashFlow - res.FinancingCashFlow
	}

	res.ExpenseStructure = expenseStructure(body, res.TotalExpense)
	res.CashFlow = monthlyCashFlow(opening, body)
	res.BalanceSeries, res.CashGap = balanceAndGap(opening, res.Period.From, body)
	res.Alerts = buildAlerts(res)

	return res
}

// signed возвращает знаковую сумму: приход +, расход -.
func signed(tx models.Transaction) float64 {
	if tx.Direction == models.Out {
		return -tx.Amount
	}
	return tx.Amount
}

// isOpeningBalance распознаёт строку входящего остатка.
func isOpeningBalance(tx models.Transaction) bool {
	cp := strings.ToLower(tx.Counterparty)
	p := strings.ToLower(tx.Purpose)
	return strings.Contains(cp, "входящий остаток") ||
		strings.Contains(p, "остаток на начало") ||
		strings.Contains(p, "входящий остаток")
}

// expenseStructure группирует расходы по Category (если заполнена) или по Counterparty.
func expenseStructure(body []models.Transaction, totalExpense float64) []models.ExpenseCategory {
	sums := map[string]float64{}
	for _, tx := range body {
		if tx.Direction != models.Out {
			continue
		}
		key := strings.TrimSpace(tx.Category)
		if key == "" {
			key = strings.TrimSpace(tx.Counterparty)
		}
		if key == "" {
			key = "Прочее"
		}
		sums[key] += tx.Amount
	}
	out := make([]models.ExpenseCategory, 0, len(sums))
	for k, v := range sums {
		share := 0.0
		if totalExpense > 0 {
			share = v / totalExpense
		}
		out = append(out, models.ExpenseCategory{Category: k, Amount: v, Share: share})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Amount > out[j].Amount })
	return out
}

// monthlyCashFlow — приход/расход по месяцам; Balance = баланс на конец месяца.
func monthlyCashFlow(opening float64, body []models.Transaction) []models.CashFlowPoint {
	type agg struct{ in, out float64 }
	m := map[string]*agg{}
	for _, tx := range body {
		key := tx.Date.Format("2006-01")
		a := m[key]
		if a == nil {
			a = &agg{}
			m[key] = a
		}
		if tx.Direction == models.In {
			a.in += tx.Amount
		} else {
			a.out += tx.Amount
		}
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	running := opening
	out := make([]models.CashFlowPoint, 0, len(keys))
	for _, k := range keys {
		a := m[k]
		running += a.in - a.out
		out = append(out, models.CashFlowPoint{Period: k, Inflow: a.in, Outflow: a.out, Balance: running})
	}
	return out
}

// balanceAndGap строит дневную линию баланса и находит кассовый разрыв (минимум < 0).
func balanceAndGap(opening float64, from time.Time, body []models.Transaction) ([]models.BalancePoint, *models.CashGap) {
	series := []models.BalancePoint{{Date: from, Balance: opening}}
	bal := opening
	minBal := opening
	minDate := from

	for i := 0; i < len(body); {
		day := body[i].Date
		for i < len(body) && body[i].Date.Equal(day) {
			bal += signed(body[i])
			i++
		}
		series = append(series, models.BalancePoint{Date: day, Balance: bal})
		if bal < minBal {
			minBal = bal
			minDate = day
		}
	}

	if minBal < 0 {
		return series, &models.CashGap{
			Date:             minDate,
			ProjectedBalance: minBal,
			Shortfall:        -minBal,
			Reason:           gapReason(body, minDate),
		}
	}
	return series, nil
}

// gapReason описывает причину разрыва: крупнейшие списания за 3 дня до минимума включительно.
func gapReason(body []models.Transaction, day time.Time) string {
	windowStart := day.AddDate(0, 0, -3)
	var exp []models.Transaction
	for _, tx := range body {
		if tx.Direction == models.Out && !tx.Date.Before(windowStart) && !tx.Date.After(day) {
			exp = append(exp, tx)
		}
	}
	sort.SliceStable(exp, func(i, j int) bool { return exp[i].Amount > exp[j].Amount })
	if len(exp) == 0 {
		return "крупные списания в этот период"
	}
	parts := make([]string, 0, 2)
	for i, tx := range exp {
		if i >= 2 {
			break
		}
		label := tx.Purpose
		if label == "" {
			label = tx.Counterparty
		}
		parts = append(parts, fmt.Sprintf("«%s» (%.0f ₽)", label, tx.Amount))
	}
	return "совпали крупные списания: " + strings.Join(parts, " и ")
}

// buildAlerts формирует список рисков/наблюдений.
func buildAlerts(res models.AuditResult) []models.Alert {
	var a []models.Alert
	if res.CashGap != nil {
		a = append(a, models.Alert{
			Severity: models.SeverityDanger,
			Message: fmt.Sprintf("Кассовый разрыв %s: баланс уходит в минус, не хватает %.0f ₽",
				res.CashGap.Date.Format("02.01.2006"), res.CashGap.Shortfall),
		})
	}
	if res.NetCashFlow < 0 {
		a = append(a, models.Alert{
			Severity: models.SeverityWarning,
			Message:  fmt.Sprintf("Расходы за период превышают доходы на %.0f ₽", -res.NetCashFlow),
		})
	}
	if len(res.ExpenseStructure) > 0 {
		top := res.ExpenseStructure[0]
		a = append(a, models.Alert{
			Severity: models.SeverityInfo,
			Message:  fmt.Sprintf("Крупнейшая статья расходов: %s (%.0f%% всех расходов)", top.Category, top.Share*100),
		})
	}
	return a
}
