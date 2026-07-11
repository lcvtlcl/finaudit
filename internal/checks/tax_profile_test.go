package checks

import (
	"testing"
	"time"

	"github.com/wwpp/finaudit/internal/models"
)

func hasCheck(checks []models.Check, code string) bool {
	for _, c := range checks {
		if c.Code == code {
			return true
		}
	}
	return false
}

func testAuditResult() models.AuditResult {
	return models.AuditResult{
		Period: models.Period{
			From: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			To:   time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC),
		},
		OpeningBalance:    100000,
		ClosingBalance:    120000,
		TotalIncome:       300000,
		TotalExpense:      150000,
		NetCashFlow:       150000,
		OperatingCashFlow: 150000,
	}
}

func TestRun_USN_HasUSNChecks(t *testing.T) {
	res := testAuditResult()
	got := Run(res, nil, models.TaxProfile{TaxRegime: "usn"})

	if !hasCheck(got, "TAX_RESERVE") {
		t.Fatal("expected TAX_RESERVE for usn")
	}
	if !hasCheck(got, "VAT_THRESHOLD") {
		t.Fatal("expected VAT_THRESHOLD for usn")
	}
	if hasCheck(got, "NPD_CONTEXT") {
		t.Fatal("did not expect NPD_CONTEXT for usn")
	}
}

func TestRun_NPD_HasOnlyNPDContext(t *testing.T) {
	res := testAuditResult()
	got := Run(res, nil, models.TaxProfile{TaxRegime: "npd"})

	if hasCheck(got, "TAX_RESERVE") {
		t.Fatal("did not expect TAX_RESERVE for npd")
	}
	if hasCheck(got, "VAT_THRESHOLD") {
		t.Fatal("did not expect VAT_THRESHOLD for npd")
	}
	if !hasCheck(got, "NPD_CONTEXT") {
		t.Fatal("expected NPD_CONTEXT for npd")
	}
}

func TestRun_OSNO_HasNoUSNOrNPDChecks(t *testing.T) {
	res := testAuditResult()
	got := Run(res, nil, models.TaxProfile{TaxRegime: "osno"})

	if hasCheck(got, "TAX_RESERVE") {
		t.Fatal("did not expect TAX_RESERVE for osno")
	}
	if hasCheck(got, "VAT_THRESHOLD") {
		t.Fatal("did not expect VAT_THRESHOLD for osno")
	}
	if hasCheck(got, "NPD_CONTEXT") {
		t.Fatal("did not expect NPD_CONTEXT for osno")
	}
}
