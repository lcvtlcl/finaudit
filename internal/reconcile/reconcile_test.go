package reconcile

import (
	"testing"
	"time"

	"github.com/wwpp/finaudit/internal/models"
)

func TestReconcile_MatchedByINNAndAmount(t *testing.T) {
	txs := []models.Transaction{
		{
			Date:         time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC),
			Amount:       15000.50,
			Counterparty: "ООО Ромашка",
			INN:          "7701234567",
		},
	}
	docs := []Document{
		{
			Number:       "123",
			Date:         "10.07.2026",
			Amount:       15000.50,
			Counterparty: "ООО Ромашка",
			INN:          "7701234567",
		},
	}

	got := Reconcile(txs, docs)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Status != MatchOK {
		t.Fatalf("status = %q, want %q", got[0].Status, MatchOK)
	}
	if got[0].Diff != 0 {
		t.Fatalf("diff = %v, want 0", got[0].Diff)
	}
}

func TestReconcile_NoDocument(t *testing.T) {
	txs := []models.Transaction{
		{
			Date:         time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC),
			Amount:       2000,
			Counterparty: "ООО НетДок",
			INN:          "7700000001",
		},
	}

	got := Reconcile(txs, nil)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Status != MatchNoDoc {
		t.Fatalf("status = %q, want %q", got[0].Status, MatchNoDoc)
	}
}

func TestReconcile_NoPayment(t *testing.T) {
	docs := []Document{
		{
			Number:       "77",
			Date:         "10.07.2026",
			Amount:       3000,
			Counterparty: "ООО БезОплаты",
			INN:          "7700000002",
		},
	}

	got := Reconcile(nil, docs)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Status != MatchNoPayment {
		t.Fatalf("status = %q, want %q", got[0].Status, MatchNoPayment)
	}
}

func TestReconcile_AmountMismatch(t *testing.T) {
	txs := []models.Transaction{
		{
			Date:         time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC),
			Amount:       1000,
			Counterparty: "ООО Тест",
			INN:          "7701234567",
		},
	}
	docs := []Document{
		{
			Number:       "1",
			Date:         "10.07.2026",
			Amount:       1200,
			Counterparty: "ООО Тест",
			INN:          "7701234567",
		},
	}

	got := Reconcile(txs, docs)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Status != MatchAmountGap {
		t.Fatalf("status = %q, want %q", got[0].Status, MatchAmountGap)
	}
	if got[0].Diff != -200 {
		t.Fatalf("diff = %v, want -200", got[0].Diff)
	}
}
