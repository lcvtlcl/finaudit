package ingest

import (
	"os"
	"testing"
)

func TestSimGeneratedTxtParses(t *testing.T) {
	personas := []string{"ip_retail_usn6", "ooo_services_usn15", "ecom_marketplace"}
	for _, p := range personas {
		for q := 1; q <= 4; q++ {
			path := "testdata/sim/" + p + "/q" + string(rune('0'+q)) + ".txt"
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			txs, err := parseClientBankExchange(raw)
			if err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
			if len(txs) == 0 {
				t.Errorf("%s: пустой список транзакций", path)
			}
		}
	}
}
