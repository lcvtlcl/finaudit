package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/wwpp/finaudit/internal/models"
)

var personas = []string{"ip_retail_usn6", "ooo_services_usn15", "ecom_marketplace"}

func main() {
	seed := flag.Int64("seed", 42, "детерминированный seed")
	out := flag.String("out", "internal/ingest/testdata/sim", "выходная директория")
	only := flag.String("persona", "", "сгенерировать только одну персону (опционально)")
	flag.Parse()

	list := personas
	if *only != "" {
		list = []string{*only}
	}

	for _, persona := range list {
		if err := generateForPersona(*seed, persona, *out); err != nil {
			log.Fatalf("персона %s: %v", persona, err)
		}
		fmt.Printf("готово: %s\n", persona)
	}
}

func generateForPersona(seed int64, persona, outDir string) error {
	txs := GeneratePersona(seed, persona)
	dir := filepath.Join(outDir, persona)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	byQuarter := map[int][]models.Transaction{}
	for _, tx := range txs {
		q := quarterOf(tx.Date)
		byQuarter[q] = append(byQuarter[q], tx)
	}

	account := "40802810000000012345"

	for q := 1; q <= 4; q++ {
		qtxs := byQuarter[q]
		base := filepath.Join(dir, fmt.Sprintf("q%d", q))

		if err := writeTxt(base+".txt", qtxs, account); err != nil {
			return fmt.Errorf("txt q%d: %w", q, err)
		}
		if err := writeCSV(base+".csv", qtxs); err != nil {
			return fmt.Errorf("csv q%d: %w", q, err)
		}
		if err := writeXLSX(base+".xlsx", qtxs); err != nil {
			return fmt.Errorf("xlsx q%d: %w", q, err)
		}
	}

	annual := buildAnnualExpected(persona, txs)
	if err := writeAnnualExpected(filepath.Join(dir, "expected_annual.json"), annual); err != nil {
		return fmt.Errorf("expected_annual: %w", err)
	}

	return nil
}
