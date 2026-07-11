package categorize

import (
	"testing"
	"time"

	"github.com/wwpp/finaudit/internal/models"
)

func makeTx(date time.Time, amount float64, dir models.Direction, cp, inn, purpose string) models.Transaction {
	return models.Transaction{
		Date:         date,
		Amount:       amount,
		Direction:    dir,
		Counterparty: cp,
		INN:          inn,
		Purpose:      purpose,
	}
}

func TestDetectRecurring_MonthlyRent(t *testing.T) {
	var txs []models.Transaction
	for m := 1; m <= 6; m++ {
		txs = append(txs, makeTx(
			time.Date(2026, time.Month(m), 5, 10, 0, 0, 0, time.UTC),
			90000, models.Out, "Арендодатель ООО «Пассаж»", "7701234567", "Аренда торговой точки",
		))
	}

	result := DetectRecurring(txs)
	if len(result) == 0 {
		t.Fatal("ожидался хотя бы один регулярный платёж")
	}

	found := false
	for _, rp := range result {
		if rp.Counterparty == "Арендодатель ООО «Пассаж»" && rp.Occurrences == 6 {
			found = true
			if rp.PeriodDays != 30 {
				t.Errorf("ожидался periodDays=30, получено %d", rp.PeriodDays)
			}
			if rp.AvgAmount != 90000 {
				t.Errorf("ожидалась сумма 90000, получено %v", rp.AvgAmount)
			}
		}
	}
	if !found {
		t.Error("аренда не обнаружена как регулярный платёж")
	}
}

func TestDetectRecurring_IgnoresOneOffPayment(t *testing.T) {
	txs := []models.Transaction{
		makeTx(time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC), 950000, models.Out, "Поставщик ООО «Субподрядчик Плюс»", "5029887766", "Предоплата"),
	}
	result := DetectRecurring(txs)
	if len(result) != 0 {
		t.Errorf("одноразовый платёж не должен считаться регулярным, получено %d", len(result))
	}
}

func TestDetectRecurring_AmountToleranceAllowsSmallVariation(t *testing.T) {
	amounts := []float64{65000, 66500, 64200, 65800, 65100, 66000}
	var txs []models.Transaction
	for i, amt := range amounts {
		txs = append(txs, makeTx(
			time.Date(2026, time.Month(i+1), 25, 10, 0, 0, 0, time.UTC),
			amt, models.Out, "Иванова А.С.", "", "Зарплата продавцу за месяц",
		))
	}
	result := DetectRecurring(txs)
	if len(result) != 1 {
		t.Fatalf("ожидался 1 регулярный платёж, получено %d", len(result))
	}
	if result[0].Occurrences != 6 {
		t.Errorf("ожидалось 6 повторов, получено %d", result[0].Occurrences)
	}
}
