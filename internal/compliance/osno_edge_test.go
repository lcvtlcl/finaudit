package compliance

import (
	"testing"
	"time"

	"github.com/wwpp/finaudit/internal/models"
)

func TestCompliance_OsnoZeroIncome(t *testing.T) {
	txs := []models.Transaction{
		{
			Date:      time.Date(2026, 4, 15, 0, 0, 0, 0, time.UTC),
			Amount:    200_000,
			Direction: models.Out,
			Category:  "налоги",
		},
	}

	res := models.AuditResult{
		Period: models.Period{
			From: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			To:   time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC),
		},
		TotalIncome:  0,
		TotalExpense: 200_000,
		NetCashFlow:  -200_000,
	}

	profile := models.TaxProfile{TaxRegime: "osno"}
	flags := Run(res, txs, nil, profile)

	if len(flags) == 0 {
		t.Fatalf("expected OSNO compliance flags for zero income + taxes, got none")
	}
}
