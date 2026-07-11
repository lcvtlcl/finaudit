package metrics

import (
	"testing"
	"time"

	"github.com/wwpp/finaudit/internal/models"
)

func TestMetrics1_ExcludesInternalTransfers(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	txs := []models.Transaction{
		{Date: base, Amount: 100000, Direction: models.In, Counterparty: "ООО Клиент", Purpose: "Оплата по договору №1"},
		{Date: base.AddDate(0, 0, 1), Amount: 50000, Direction: models.In, Counterparty: "Иванов И.И.", Purpose: "Взнос учредителя на пополнение оборотных средств"},
		{Date: base.AddDate(0, 0, 2), Amount: 30000, Direction: models.Out, Counterparty: "Иванов И.И.", Purpose: "Возврат займа по договору беспроцентного займа от 01.01.2026"},
	}

	res := ComputeAudit(txs)

	if res.TotalIncome != 100000 {
		t.Errorf("TotalIncome = %v, want 100000 (взнос учредителя должен быть исключён)", res.TotalIncome)
	}
	if res.TotalExpense != 0 {
		t.Errorf("TotalExpense = %v, want 0 (возврат займа должен быть исключён)", res.TotalExpense)
	}
	if res.ExcludedTransfers != 80000 {
		t.Errorf("ExcludedTransfers = %v, want 80000 (50000+30000)", res.ExcludedTransfers)
	}
	// Баланс/движение денег не должно зависеть от классификации выручки.
	wantNetCash := 100000.0 + 50000.0 - 30000.0
	gotNetCash := res.ClosingBalance - res.OpeningBalance
	if gotNetCash != wantNetCash {
		t.Errorf("движение по балансу = %v, want %v (внутренние переводы всё равно двигают деньги)", gotNetCash, wantNetCash)
	}
}

func TestMetrics2_NetsRefundsAgainstOriginalDirection(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	txs := []models.Transaction{
		{Date: base, Amount: 20000, Direction: models.Out, Counterparty: "ООО Поставщик", Purpose: "Оплата за товар по счёту №5"},
		{Date: base.AddDate(0, 0, 1), Amount: 5000, Direction: models.In, Counterparty: "ООО Поставщик", Purpose: "Возврат излишне уплаченных денежных средств"},
		{Date: base.AddDate(0, 0, 2), Amount: 15000, Direction: models.In, Counterparty: "ООО Клиент", Purpose: "Оплата по договору №2"},
		{Date: base.AddDate(0, 0, 3), Amount: 3000, Direction: models.Out, Counterparty: "ООО Клиент", Purpose: "Возврат денежных средств за некачественный товар"},
	}

	res := ComputeAudit(txs)

	// 20000 (расход) - 5000 (возврат от поставщика, гасит расход) = 15000
	if res.TotalExpense != 15000 {
		t.Errorf("TotalExpense = %v, want 15000 (возврат от поставщика должен уменьшить расход, а не увеличить доход)", res.TotalExpense)
	}
	// 15000 (доход) - 3000 (возврат клиенту, гасит доход) = 12000
	if res.TotalIncome != 12000 {
		t.Errorf("TotalIncome = %v, want 12000 (возврат клиенту должен уменьшить доход, а не увеличить расход)", res.TotalIncome)
	}
	if res.NetRefunds != 8000 {
		t.Errorf("NetRefunds = %v, want 8000 (5000+3000)", res.NetRefunds)
	}
}

func TestMetrics1And2_DoNotAffectBalanceSeries(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	txs := []models.Transaction{
		{Date: base, Amount: 10000, Direction: models.In, Counterparty: "Иванов И.И.", Purpose: "Взнос учредителя"},
		{Date: base.AddDate(0, 0, 1), Amount: 2000, Direction: models.Out, Counterparty: "ООО Х", Purpose: "Возврат денежных средств"},
	}

	res := ComputeAudit(txs)

	if len(res.BalanceSeries) == 0 {
		t.Fatal("BalanceSeries не должна быть пустой")
	}
	last := res.BalanceSeries[len(res.BalanceSeries)-1]
	wantBalance := 10000.0 - 2000.0
	if last.Balance != wantBalance {
		t.Errorf("последний баланс = %v, want %v (сторно/переводы всё равно двигают реальные деньги)", last.Balance, wantBalance)
	}
}
