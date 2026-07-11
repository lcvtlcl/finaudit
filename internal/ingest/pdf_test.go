package ingest

import "testing"

func TestParsePDF_RealSample(t *testing.T) {
	txs, err := ParsePDF("testdata/real/pdf/sample.pdf")
	if err != nil {
		t.Fatalf("ParsePDF: %v", err)
	}
	if len(txs) == 0 {
		t.Error("ожидались транзакции из PDF")
	}
	for _, tx := range txs {
		if tx.Amount <= 0 {
			t.Errorf("некорректная сумма: %v", tx.Amount)
		}
		if tx.Date.IsZero() {
			t.Error("некорректная дата")
		}
	}
}
