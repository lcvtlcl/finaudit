package checks

import (
	"testing"
	"time"

	"github.com/wwpp/finaudit/internal/models"
)

func TestChecks_OsnoBasic(t *testing.T) {
	txs := []models.Transaction{
		{
			Date:      time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC),
			Amount:    1_000_000,
			Direction: models.In,
			Category:  "выручка",
		},
		{
			Date:      time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC),
			Amount:    700_000,
			Direction: models.Out,
			Category:  "закупки",
		},
	}

	res := models.AuditResult{
		Period: models.Period{
			From: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			To:   time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC),
		},
		TotalIncome:  1_000_000,
		TotalExpense: 700_000,
		NetCashFlow:  300_000,
	}

	profile := models.TaxProfile{TaxRegime: "osno"}
	flags := Run(res, txs, profile)

	if len(flags) == 0 {
		t.Fatalf("expected some OSNO flags, got none")
	}
}
