// Package forecast строит прогноз баланса вперёд по регулярным платежам,
// найденным в истории транзакций. Полностью детерминированно (без LLM):
// код находит периодичность, проецирует баланс и ищет прогнозный кассовый разрыв.
package forecast

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/wwpp/finaudit/internal/categorize"
	"github.com/wwpp/finaudit/internal/models"
)

// Build возвращает прогноз баланса на horizonDays вперёд от asOf (конец периода выписки).
// Если регулярных платежей не найдено — возвращает nil (прогнозировать нечего).
func Build(txs []models.Transaction, closingBalance float64, asOf time.Time, horizonDays int, planned []models.PlannedPayment) *models.Forecast {
	if horizonDays <= 0 {
		horizonDays = 60
	}
	rec := categorize.DetectRecurring(txs)
	if len(rec) == 0 && len(planned) == 0 {
		return nil
	}

	series := make([]models.BalancePoint, 0, horizonDays)
	bal := closingBalance
	var gap *models.CashGap

	for d := 1; d <= horizonDays; d++ {
		day := asOf.AddDate(0, 0, d)
		var net float64
		for _, rp := range rec {
			if occursOn(rp, day) {
				if rp.Direction == models.In {
					net += rp.AvgAmount
				} else {
					net -= rp.AvgAmount
				}
			}
		}
		for _, pp := range planned {
			if sameDay(pp.Date, day) {
				if pp.Direction == models.In {
					net += pp.Amount
				} else {
					net -= pp.Amount
				}
			}
		}
		bal += net
		series = append(series, models.BalancePoint{Date: day, Balance: bal})
		if gap == nil && bal < 0 {
			gap = &models.CashGap{
				Date:             day,
				ProjectedBalance: bal,
				Shortfall:        -bal,
				Reason:           gapReason(rec, day),
			}
		}
	}

	return &models.Forecast{
		HorizonDays: horizonDays,
		Series:      series,
		Recurring:   rec,
		Gap:         gap,
	}
}

// sameDay сравнивает даты по календарному дню (без времени).
func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// occursOn — true, если регулярный платёж выпадает на этот день (NextDate + k*period).
func occursOn(rp models.RecurringPayment, day time.Time) bool {
	if rp.PeriodDays <= 0 {
		return false
	}
	diff := int(math.Round(day.Sub(rp.NextDate).Hours() / 24))
	return diff >= 0 && diff%rp.PeriodDays == 0
}

// gapReason описывает, какие крупные списания привели к прогнозному разрыву.
func gapReason(rec []models.RecurringPayment, gapDay time.Time) string {
	type hit struct {
		name string
		amt  float64
	}
	var hits []hit
	for _, rp := range rec {
		if rp.Direction != models.Out {
			continue
		}
		for d := 0; d <= 3; d++ {
			if occursOn(rp, gapDay.AddDate(0, 0, -d)) {
				hits = append(hits, hit{rp.Counterparty, rp.AvgAmount})
				break
			}
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].amt > hits[j].amt })
	if len(hits) == 0 {
		return "накопление регулярных списаний к этой дате"
	}
	parts := make([]string, 0, 2)
	for i, h := range hits {
		if i >= 2 {
			break
		}
		parts = append(parts, fmt.Sprintf("%s (%.0f ₽)", h.name, h.amt))
	}
	return "ожидаемые списания: " + strings.Join(parts, ", ")
}
