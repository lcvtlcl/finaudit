package compliance

import (
	"testing"
	"time"

	"github.com/wwpp/finaudit/internal/models"
)

func hasFlag(flags []models.ComplianceFlag, code string) bool {
	for _, f := range flags {
		if f.Code == code {
			return true
		}
	}
	return false
}

func testComplianceResult() models.AuditResult {
	return models.AuditResult{
		Period: models.Period{
			From: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			To:   time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC),
		},
		TotalIncome:  300000,
		TotalExpense: 150000,
	}
}

func TestRun_USN_HasUSNFlags(t *testing.T) {
	res := testComplianceResult()
	got := Run(res, nil, nil, models.TaxProfile{TaxRegime: "usn"})

	if !hasFlag(got, "VAT_THRESHOLD") {
		t.Fatal("expected VAT_THRESHOLD for usn")
	}
	if !hasFlag(got, "USN_LIMIT") {
		t.Fatal("expected USN_LIMIT for usn")
	}
	if hasFlag(got, "NPD_CONTEXT") {
		t.Fatal("did not expect NPD_CONTEXT for usn")
	}
}

func TestRun_NPD_HasOnlyNPDContext(t *testing.T) {
	res := testComplianceResult()
	got := Run(res, nil, nil, models.TaxProfile{TaxRegime: "npd"})

	if hasFlag(got, "VAT_THRESHOLD") {
		t.Fatal("did not expect VAT_THRESHOLD for npd")
	}
	if hasFlag(got, "USN_LIMIT") {
		t.Fatal("did not expect USN_LIMIT for npd")
	}
	if !hasFlag(got, "NPD_CONTEXT") {
		t.Fatal("expected NPD_CONTEXT for npd")
	}
}

func TestRun_OSNO_HasNoUSNOrNPDFlags(t *testing.T) {
	res := testComplianceResult()
	got := Run(res, nil, nil, models.TaxProfile{TaxRegime: "osno"})

	if hasFlag(got, "VAT_THRESHOLD") {
		t.Fatal("did not expect VAT_THRESHOLD for osno")
	}
	if hasFlag(got, "USN_LIMIT") {
		t.Fatal("did not expect USN_LIMIT for osno")
	}
	if hasFlag(got, "NPD_CONTEXT") {
		t.Fatal("did not expect NPD_CONTEXT for osno")
	}
}
