package checks

import (
	"testing"

	"github.com/wwpp/finaudit/internal/categorize"
	"github.com/wwpp/finaudit/internal/ingest"
	"github.com/wwpp/finaudit/internal/metrics"
	"github.com/wwpp/finaudit/internal/models"
)

func runChecks(t *testing.T) []models.Check {
	t.Helper()
	txs, err := ingest.ParseFile("../../testdata/statement_q2_2026.csv")
	if err != nil {
		t.Fatalf("парсинг выписки: %v", err)
	}
	txs = categorize.Categorize(txs)
	res := metrics.ComputeAudit(txs)
	return Run(res, txs, models.TaxProfile{TaxRegime: "usn"})
}

func byCode(checks []models.Check, code string) *models.Check {
	for i := range checks {
		if checks[i].Code == code {
			return &checks[i]
		}
	}
	return nil
}

func TestChecksCount(t *testing.T) {
	c := runChecks(t)
	if len(c) != 10 {
		t.Fatalf("ожидалось 10 проверок, получено %d", len(c))
	}
}

func TestCashGapCheck(t *testing.T) {
	c := byCode(runChecks(t), "CASH_GAP")
	if c == nil {
		t.Fatal("нет проверки CASH_GAP")
	}
	if c.Status != models.CheckDanger {
		t.Errorf("CASH_GAP: статус %q, ожидался danger", c.Status)
	}
}

func TestOperatingHealthCheck(t *testing.T) {
	c := byCode(runChecks(t), "OPERATING_HEALTH")
	if c == nil {
		t.Fatal("нет проверки OPERATING_HEALTH")
	}
	if c.Status != models.CheckOK {
		t.Errorf("OPERATING_HEALTH: статус %q, ожидался ok (операционный поток +37000)", c.Status)
	}
}

func TestTaxesCheck(t *testing.T) {
	c := byCode(runChecks(t), "TAXES")
	if c == nil {
		t.Fatal("нет проверки TAXES")
	}
	if c.Status != models.CheckOK {
		t.Errorf("TAXES: статус %q, ожидался ok (налог УСН в выписке есть)", c.Status)
	}
}

func TestAllChecksHaveTitleAndStatus(t *testing.T) {
	for _, c := range runChecks(t) {
		if c.Code == "" || c.Title == "" || c.Status == "" {
			t.Errorf("проверка с пустыми полями: %+v", c)
		}
	}
}
