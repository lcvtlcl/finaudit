package ingest

import (
	"testing"

	"github.com/wwpp/finaudit/internal/models"
)

func TestParseBankStatement(t *testing.T) {
	transactions, err := ParseFile("../../testdata/bank_statement_q2_2026.csv")
	if err != nil {
		t.Fatalf("ошибка парсинга bank_statement CSV: %v", err)
	}

	if len(transactions) == 0 {
		t.Fatal("не распарсилась ни одна транзакция")
	}

	// Проверяем, что у всех транзакций Amount > 0 (модуль)
	for i, tx := range transactions {
		if tx.Amount <= 0 {
			t.Fatalf("транзакция %d: сумма должна быть положительной, получено %.2f", i, tx.Amount)
		}
	}

	// Проверяем, что Direction только In или Out
	for i, tx := range transactions {
		if tx.Direction != models.In && tx.Direction != models.Out {
			t.Fatalf("транзакция %d: направление должно быть In или Out, получено %s", i, tx.Direction)
		}
	}

	// Проверяем, что Date не нулевая
	for i, tx := range transactions {
		if tx.Date.IsZero() {
			t.Fatalf("транзакция %d: дата не должна быть нулевой", i)
		}
	}

	// Проверяем, что хотя бы у одной непустая Category
	foundCategory := false
	for _, tx := range transactions {
		if tx.Category != "" {
			foundCategory = true
			break
		}
	}
	if !foundCategory {
		t.Fatal("не найдено ни одной транзакции с непустой категорией")
	}
}
