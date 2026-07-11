package categorize

import (
	"math"
	"sort"
	"time"

	"github.com/wwpp/finaudit/internal/models"
)

const (
	amountTolerance = 0.12 // ±12% на сумму — покрывает индексацию/округления
	monthTolDays    = 5    // допуск для месячной периодичности
	quarterTolDays  = 7    // допуск для квартальной периодичности
	minOccurrences  = 3    // минимум повторов, чтобы считать платёж регулярным
)

// recurringKey — ключ группировки: контрагент+ИНН.
type recurringKey struct {
	Counterparty string
	INN          string
	Direction    models.Direction
}

// DetectRecurring находит повторяющиеся операции по истории транзакций.
// Ключ: контрагент+ИНН, похожая сумма (±допуск), периодичность месяц/квартал ±несколько дней.
// Не изменяет входные транзакции.
func DetectRecurring(txs []models.Transaction) []models.RecurringPayment {
	groups := groupByCounterparty(txs)

	var result []models.RecurringPayment
	for key, group := range groups {
		sort.Slice(group, func(i, j int) bool { return group[i].Date.Before(group[j].Date) })
		clusters := clusterByAmount(group)
		for _, cluster := range clusters {
			if len(cluster) < minOccurrences {
				continue
			}
			rp, ok := buildRecurringPayment(key, cluster)
			if ok {
				result = append(result, rp)
			}
		}
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].Category != result[j].Category {
			return result[i].Category < result[j].Category
		}
		return result[i].NextDate.Before(result[j].NextDate)
	})

	return result
}

func groupByCounterparty(txs []models.Transaction) map[recurringKey][]models.Transaction {
	groups := map[recurringKey][]models.Transaction{}
	for _, tx := range txs {
		key := recurringKey{
			Counterparty: tx.Counterparty,
			INN:          tx.INN,
			Direction:    tx.Direction,
		}
		groups[key] = append(groups[key], tx)
	}
	return groups
}

// clusterByAmount разбивает транзакции одного контрагента на подгруппы по похожей сумме (±tolerance).
func clusterByAmount(txs []models.Transaction) [][]models.Transaction {
	sort.Slice(txs, func(i, j int) bool { return txs[i].Amount < txs[j].Amount })

	var clusters [][]models.Transaction
	var current []models.Transaction

	for _, tx := range txs {
		if len(current) == 0 {
			current = []models.Transaction{tx}
			continue
		}
		avg := avgAmount(current)
		if withinTolerance(tx.Amount, avg, amountTolerance) {
			current = append(current, tx)
		} else {
			clusters = append(clusters, current)
			current = []models.Transaction{tx}
		}
	}
	if len(current) > 0 {
		clusters = append(clusters, current)
	}
	return clusters
}

func avgAmount(txs []models.Transaction) float64 {
	var sum float64
	for _, tx := range txs {
		sum += tx.Amount
	}
	return sum / float64(len(txs))
}

func withinTolerance(amount, avg, tolerance float64) bool {
	if avg == 0 {
		return amount == 0
	}
	diff := math.Abs(amount-avg) / avg
	return diff <= tolerance
}

// buildRecurringPayment проверяет периодичность (месяц/квартал ±допуск) и строит прогноз следующего платежа.
func buildRecurringPayment(key recurringKey, cluster []models.Transaction) (models.RecurringPayment, bool) {
	sort.Slice(cluster, func(i, j int) bool { return cluster[i].Date.Before(cluster[j].Date) })

	intervals := make([]float64, 0, len(cluster)-1)
	for i := 1; i < len(cluster); i++ {
		days := cluster[i].Date.Sub(cluster[i-1].Date).Hours() / 24
		intervals = append(intervals, days)
	}
	if len(intervals) == 0 {
		return models.RecurringPayment{}, false
	}

	avgInterval := avgFloat(intervals)

	var periodDays int
	switch {
	case withinDayTolerance(avgInterval, 30, monthTolDays) || withinDayTolerance(avgInterval, 31, monthTolDays):
		periodDays = 30
	case withinDayTolerance(avgInterval, 90, quarterTolDays) || withinDayTolerance(avgInterval, 91, quarterTolDays):
		periodDays = 90
	default:
		return models.RecurringPayment{}, false
	}

	// проверяем, что интервалы не расползлись слишком сильно (иначе это не регулярный платёж)
	for _, d := range intervals {
		if math.Abs(d-avgInterval) > float64(quarterTolDays)+2 {
			return models.RecurringPayment{}, false
		}
	}

	last := cluster[len(cluster)-1]
	nextDate := last.Date.Add(time.Duration(periodDays) * 24 * time.Hour)

	rp := models.RecurringPayment{
		Counterparty: key.Counterparty,
		Category:     detectCategory(cluster),
		Direction:    key.Direction,
		AvgAmount:    round2(avgAmount(cluster)),
		PeriodDays:   periodDays,
		NextDate:     nextDate,
		Occurrences:  len(cluster),
	}
	return rp, true
}

// detectCategory переиспользует существующую Categorize(), чтобы категории совпадали с остальной системой.
func detectCategory(cluster []models.Transaction) string {
	categorized := Categorize(cluster)
	counts := map[string]int{}
	for _, tx := range categorized {
		counts[tx.Category]++
	}
	best := ""
	bestCount := 0
	for cat, cnt := range counts {
		if cnt > bestCount {
			best = cat
			bestCount = cnt
		}
	}
	return best
}

func avgFloat(vals []float64) float64 {
	var sum float64
	for _, v := range vals {
		sum += v
	}
	return sum / float64(len(vals))
}

func withinDayTolerance(value, target, tolerance float64) bool {
	return math.Abs(value-target) <= tolerance
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}
