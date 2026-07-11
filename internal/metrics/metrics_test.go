package metrics

import (
	"testing"

	"github.com/wwpp/finaudit/internal/categorize"
	"github.com/wwpp/finaudit/internal/ingest"
	"github.com/wwpp/finaudit/internal/models"
)

func loadAudit(t *testing.T) models.AuditResult {
	t.Helper()
	txs, err := ingest.ParseFile("../../testdata/statement_q2_2026.csv")
	if err != nil {
		t.Fatalf("парсинг выписки: %v", err)
	}
	txs = categorize.Categorize(txs)
	return ComputeAudit(txs)
}

func TestTotals(t *testing.T) {
	r := loadAudit(t)
	cases := []struct {
		name string
		got  float64
		want float64
	}{
		{"OpeningBalance", r.OpeningBalance, 150000},
		{"TotalIncome", r.TotalIncome, 1154000},
		{"TotalExpense", r.TotalExpense, 1297000},
		{"NetCashFlow", r.NetCashFlow, -143000},
		{"ClosingBalance", r.ClosingBalance, 7000},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: получено %.2f, ожидалось %.2f", c.name, c.got, c.want)
		}
	}
}

func TestPeriod(t *testing.T) {
	r := loadAudit(t)
	if got := r.Period.From.Format("2006-01-02"); got != "2026-04-01" {
		t.Errorf("Period.From: %s, ожидалось 2026-04-01", got)
	}
	if got := r.Period.To.Format("2006-01-02"); got != "2026-06-30" {
		t.Errorf("Period.To: %s, ожидалось 2026-06-30", got)
	}
}

func TestCashGap(t *testing.T) {
	r := loadAudit(t)
	if r.CashGap == nil {
		t.Fatal("ожидался кассовый разрыв, получено nil")
	}
	if got := r.CashGap.Date.Format("2006-01-02"); got != "2026-06-22" {
		t.Errorf("CashGap.Date: %s, ожидалось 2026-06-22", got)
	}
	if r.CashGap.ProjectedBalance != -102000 {
		t.Errorf("CashGap.ProjectedBalance: %.2f, ожидалось -102000", r.CashGap.ProjectedBalance)
	}
	if r.CashGap.Shortfall != 102000 {
		t.Errorf("CashGap.Shortfall: %.2f, ожидалось 102000", r.CashGap.Shortfall)
	}
	if r.CashGap.Reason == "" {
		t.Error("CashGap.Reason пустой")
	}
}

func TestExpenseStructureTop(t *testing.T) {
	r := loadAudit(t)
	if len(r.ExpenseStructure) == 0 {
		t.Fatal("структура расходов пустая")
	}
	top := r.ExpenseStructure[0]
	if top.Category != "ФОТ" {
		t.Errorf("крупнейшая статья: %q, ожидалось 'ФОТ'", top.Category)
	}
	if top.Amount != 381000 {
		t.Errorf("сумма крупнейшей статьи: %.2f, ожидалось 381000", top.Amount)
	}
	// доли должны суммироваться в ~1.0
	var sumShare float64
	for _, e := range r.ExpenseStructure {
		sumShare += e.Share
	}
	if sumShare < 0.999 || sumShare > 1.001 {
		t.Errorf("сумма долей расходов = %.4f, ожидалось ~1.0", sumShare)
	}
}

func TestDangerAlert(t *testing.T) {
	r := loadAudit(t)
	hasDanger := false
	for _, a := range r.Alerts {
		if a.Severity == models.SeverityDanger {
			hasDanger = true
		}
	}
	if !hasDanger {
		t.Error("ожидался алерт уровня danger про кассовый разрыв")
	}
}

func TestActivityCashFlow(t *testing.T) {
	r := loadAudit(t)
	if r.OperatingCashFlow != 37000 {
		t.Errorf("OperatingCashFlow: %.2f, ожидалось 37000", r.OperatingCashFlow)
	}
	if r.InvestingCashFlow != -180000 {
		t.Errorf("InvestingCashFlow: %.2f, ожидалось -180000", r.InvestingCashFlow)
	}
	if r.FinancingCashFlow != 0 {
		t.Errorf("FinancingCashFlow: %.2f, ожидалось 0", r.FinancingCashFlow)
	}
}
