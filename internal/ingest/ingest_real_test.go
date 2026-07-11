package ingest

import (
	"path/filepath"
	"testing"
)

func TestParseRealFixtures(t *testing.T) {
	files, err := filepath.Glob("testdata/real/*.txt")
	if err != nil {
		t.Fatalf("glob error: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no fixture files found in testdata/real/")
	}

	for _, path := range files {
		t.Run(filepath.Base(path), func(t *testing.T) {
			txs, err := ParseFile(path)
			if err != nil {
				t.Fatalf("ParseFile error: %v", err)
			}
			if len(txs) == 0 {
				t.Fatal("expected at least one transaction, got 0")
			}
			t.Logf("%s: parsed %d transactions", filepath.Base(path), len(txs))
			for i, tx := range txs {
				if tx.Amount <= 0 {
					t.Errorf("tx[%d]: amount must be > 0, got %v", i, tx.Amount)
				}
				if tx.Counterparty == "" {
					t.Errorf("tx[%d]: counterparty is empty", i)
				}
				if tx.Purpose == "" {
					t.Errorf("tx[%d]: purpose is empty", i)
				}
			}
		})
	}
}
