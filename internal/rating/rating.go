// Package rating — детерминированный балл финансового здоровья (0..100, буква A..E)
// из уже посчитанных метрик, прогноза и compliance. Считает КОД, не ИИ.
// Это индикатор здоровья, а не кредитный скоринг.
package rating

import "github.com/wwpp/finaudit/internal/models"

// Compute возвращает рейтинг по результату аудита (res.Compliance должен быть уже заполнен).
func Compute(res models.AuditResult) *models.Rating {
	score := 0
	var pos, neg []string

	// Операционное здоровье (25).
	if res.OperatingCashFlow >= 0 {
		score += 25
		pos = append(pos, "Операционный поток положительный")
	} else {
		neg = append(neg, "Операционный поток отрицательный")
	}

	// Кассовый разрыв (25).
	switch {
	case res.CashGap != nil:
		neg = append(neg, "Кассовый разрыв в периоде")
	case res.Forecast != nil && res.Forecast.Gap != nil:
		score += 15
		neg = append(neg, "Прогнозируется разрыв впереди")
	default:
		score += 25
		pos = append(pos, "Кассовых разрывов нет")
	}

	// Финансовая подушка (15).
	months := len(res.CashFlow)
	if months == 0 {
		months = 1
	}
	avgExpense := res.TotalExpense / float64(months)
	minBal := minBalance(res)
	switch {
	case avgExpense <= 0 || minBal >= avgExpense:
		score += 15
		pos = append(pos, "Есть финансовая подушка")
	case minBal >= avgExpense*0.1:
		score += 8
	default:
		neg = append(neg, "Нет резерва на просадки")
	}

	// Чистый поток (15).
	if res.NetCashFlow >= 0 {
		score += 15
		pos = append(pos, "Доходы покрывают расходы")
	} else {
		neg = append(neg, "Расходы превышают доходы")
	}

	// Диверсификация расходов (10).
	if len(res.ExpenseStructure) == 0 || res.ExpenseStructure[0].Share < 0.4 {
		score += 10
	} else {
		neg = append(neg, "Высокая концентрация расходов")
	}

	// Базовый блок «прочее» (10), корректируется правовыми рисками ниже.
	score += 10
	for _, f := range res.Compliance {
		switch f.Severity {
		case models.ComplianceRisk:
			score -= 6
			neg = append(neg, f.Title)
		case models.ComplianceAttention:
			score -= 2
		}
	}

	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	grade, label := grade(score)
	return &models.Rating{Score: score, Grade: grade, Label: label, Positives: pos, Negatives: neg}
}

func minBalance(res models.AuditResult) float64 {
	if len(res.BalanceSeries) == 0 {
		return res.ClosingBalance
	}
	m := res.BalanceSeries[0].Balance
	for _, p := range res.BalanceSeries {
		if p.Balance < m {
			m = p.Balance
		}
	}
	return m
}

func grade(score int) (string, string) {
	switch {
	case score >= 85:
		return "A", "Отличное финансовое здоровье"
	case score >= 70:
		return "B", "Хорошее финансовое здоровье"
	case score >= 55:
		return "C", "Среднее — есть над чем поработать"
	case score >= 40:
		return "D", "Слабое — заметные риски"
	default:
		return "E", "Рискованное — нужны срочные меры"
	}
}
