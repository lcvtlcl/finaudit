package export

import (
	"bytes"
	"testing"

	"github.com/wwpp/finaudit/internal/categorize"
	"github.com/wwpp/finaudit/internal/checks"
	"github.com/wwpp/finaudit/internal/ingest"
	"github.com/wwpp/finaudit/internal/metrics"
	"github.com/wwpp/finaudit/internal/models"
)

func TestToExcel(t *testing.T) {
	txs, err := ingest.ParseFile("../../testdata/statement_q2_2026.csv")
	if err != nil {
		t.Fatalf("парсинг выписки: %v", err)
	}
	txs = categorize.Categorize(txs)
	res := metrics.ComputeAudit(txs)
	res.Checks = checks.Run(res, txs, models.TaxProfile{TaxRegime: "usn"})

	data, err := ToExcel(res)
	if err != nil {
		t.Fatalf("ToExcel: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("пустой xlsx")
	}
	// .xlsx — это zip-архив, должен начинаться с сигнатуры PK.
	if !bytes.HasPrefix(data, []byte("PK")) {
		t.Errorf("ожидалась zip-сигнатура PK, получено % x", data[:4])
	}
}
