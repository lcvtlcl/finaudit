package categorize

import (
	"strings"
	"testing"

	"github.com/wwpp/finaudit/internal/ingest"
	"github.com/wwpp/finaudit/internal/models"
)

func TestCategorizeRules(t *testing.T) {
	txs, err := ingest.ParseFile("../../testdata/statement_q2_2026.csv")
	if err != nil {
		t.Fatalf("парсинг выписки: %v", err)
	}

	categorized := Categorize(txs)

	for _, tx := range categorized {
		if tx.Direction == models.Out && tx.Category == "Прочее" {
			t.Fatalf("расходная транзакция осталась в 'Прочее': %s / %s", tx.Counterparty, tx.Purpose)
		}
	}

	var foundEquip, foundTax, foundPayroll bool
	for _, tx := range categorized {
		purpose := strings.ToLower(tx.Purpose)
		switch {
		case strings.Contains(purpose, "кофемашин"):
			if tx.Category != "Оборудование" || tx.Activity != models.ActivityInvesting {
				t.Fatalf("кофемашина: получено category=%q activity=%q", tx.Category, tx.Activity)
			}
			foundEquip = true
		case strings.Contains(purpose, "налога усн") || tx.INN == "7701000000":
			if tx.Category != "Налоги" || tx.Activity != models.ActivityOperating {
				t.Fatalf("УСН: получено category=%q activity=%q", tx.Category, tx.Activity)
			}
			foundTax = true
		case strings.Contains(purpose, "заработная плата"):
			if tx.Category != "ФОТ" {
				t.Fatalf("зарплата: получено category=%q", tx.Category)
			}
			foundPayroll = true
		}
	}

	if !foundEquip {
		t.Fatal("не найдена транзакция 'Предоплата за кофемашину'")
	}
	if !foundTax {
		t.Fatal("не найдена транзакция УСН")
	}
	if !foundPayroll {
		t.Fatal("не найдена зарплатная транзакция")
	}
}
