package ingest

import (
	"fmt"
	"testing"
)

// TestParseCheckRealFiles — ручная проверка парсинга реальных банковских образцов (bank1/bank2).
// Удалить после проверки.
func TestParseCheckRealFiles(t *testing.T) {
	files := []string{
		"../../testdata/samples/open_sources/1CClientBankExchange_sample_v1.02.txt",
		"../../testdata/samples/open_sources/sberbank_debit_2107_anonymized.txt",
		"../../testdata/samples/open_sources/bank_statement_csv_example_SYNTHETIC.csv",
	}
	for _, path := range files {
		txs, err := ParseFile(path)
		fmt.Printf("\n=== %s ===\n", path)
		if err != nil {
			fmt.Println("ERROR:", err)
			continue
		}
		fmt.Printf("parsed %d transactions\n", len(txs))
		for i, tx := range txs {
			if i >= 5 {
				break
			}
			fmt.Printf("  [%d] date=%s amount=%.2f dir=%s cp=%q inn=%q purpose=%q\n",
				i, tx.Date.Format("2006-01-02"), tx.Amount, tx.Direction, tx.Counterparty, tx.INN, tx.Purpose)
		}
	}
}
