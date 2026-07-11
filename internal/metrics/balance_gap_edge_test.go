package metrics

import (
	"testing"
	"time"

	"github.com/wwpp/finaudit/internal/models"
)

func tx(date time.Time, amount float64, dir models.Direction) models.Transaction {
	return models.Transaction{Date: date, Amount: amount, Direction: dir}
}

func day(d int) time.Time {
	return time.Date(2026, 1, d, 0, 0, 0, 0, time.UTC)
}

// 1. Пустой период — не паникует, gap = nil, series содержит только opening.
func TestBalanceAndGap_EmptyPeriod(t *testing.T) {
	series, gap := balanceAndGap(1000, day(1), []models.Transaction{})

	if gap != nil {
		t.Errorf("ожидался nil gap на пустом периоде, получено %+v", gap)
	}
	if len(series) != 1 {
		t.Fatalf("ожидалась 1 точка баланса, получено %d", len(series))
	}
	if series[0].Balance != 1000 {
		t.Errorf("ожидался баланс 1000, получено %v", series[0].Balance)
	}
}

// 2. Один день с одной транзакцией.
func TestBalanceAndGap_SingleDay(t *testing.T) {
	body := []models.Transaction{tx(day(1), 500, models.In)}
	series, gap := balanceAndGap(1000, day(1), body)

	if gap != nil {
		t.Errorf("ожидался nil gap, получено %+v", gap)
	}
	last := series[len(series)-1]
	if last.Balance != 1500 {
		t.Errorf("ожидался баланс 1500, получено %v", last.Balance)
	}
}

// 3. Только доходы — gap = nil, баланс монотонно растёт.
func TestBalanceAndGap_OnlyIncome(t *testing.T) {
	body := []models.Transaction{
		tx(day(1), 100, models.In),
		tx(day(2), 200, models.In),
		tx(day(3), 300, models.In),
	}
	series, gap := balanceAndGap(0, day(1), body)

	if gap != nil {
		t.Errorf("ожидался nil gap при только доходах, получено %+v", gap)
	}
	last := series[len(series)-1]
	if last.Balance != 600 {
		t.Errorf("ожидался баланс 600, получено %v", last.Balance)
	}
}

// 4. Только расходы — баланс может уйти в минус.
func TestBalanceAndGap_OnlyExpense(t *testing.T) {
	body := []models.Transaction{
		tx(day(1), 300, models.Out),
		tx(day(2), 300, models.Out),
	}
	series, gap := balanceAndGap(500, day(1), body)

	if gap == nil {
		t.Fatal("ожидался gap: расходы (600) превышают opening (500)")
	}
	if gap.ProjectedBalance >= 0 {
		t.Errorf("ожидался отрицательный projected_balance, получено %v", gap.ProjectedBalance)
	}
	if gap.Shortfall <= 0 {
		t.Errorf("ожидался положительный shortfall, получено %v", gap.Shortfall)
	}
	_ = series
}

// 5. Разрыв в первый день — opening меньше первого крупного списания.
func TestBalanceAndGap_GapOnFirstDay(t *testing.T) {
	body := []models.Transaction{
		tx(day(1), 5000, models.Out),
		tx(day(10), 8000, models.In),
	}
	series, gap := balanceAndGap(1000, day(1), body)

	if gap == nil {
		t.Fatal("ожидался gap в первый день")
	}
	if !gap.Date.Equal(day(1)) {
		t.Errorf("ожидалась дата разрыва %v, получено %v", day(1), gap.Date)
	}
	if gap.ProjectedBalance != -4000 {
		t.Errorf("ожидался projected_balance -4000, получено %v", gap.ProjectedBalance)
	}
	_ = series
}

// Доп. инвариант: если gap != nil, то projected_balance < 0 и shortfall > 0 (из CONTRACT.md).
func TestBalanceAndGap_InvariantWhenGapExists(t *testing.T) {
	body := []models.Transaction{tx(day(1), 999999, models.Out)}
	_, gap := balanceAndGap(0, day(1), body)

	if gap == nil {
		t.Fatal("ожидался gap")
	}
	if gap.ProjectedBalance >= 0 {
		t.Error("инвариант нарушен: projected_balance должен быть < 0")
	}
	if gap.Shortfall <= 0 {
		t.Error("инвариант нарушен: shortfall должен быть > 0")
	}
}
