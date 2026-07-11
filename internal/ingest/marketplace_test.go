package ingest

import (
	"strings"
	"testing"
)

func TestParseMarketplaceCSV_Wildberries(t *testing.T) {
	csv := "Дата продажи;К перечислению Продавцу;Номер заказа\n" +
		"05.01.2026;1500,50;12345\n" +
		"10.01.2026;2300,00;12346\n"

	txs, err := ParseMarketplaceCSV(strings.NewReader(csv))
	if err != nil {
		t.Fatalf("ParseMarketplaceCSV (WB): %v", err)
	}
	if len(txs) != 2 {
		t.Fatalf("ожидалось 2 транзакции, получено %d", len(txs))
	}
	if txs[0].Amount != 1500.50 {
		t.Errorf("ожидалась сумма 1500.50, получено %v", txs[0].Amount)
	}
}

func TestParseMarketplaceCSV_Ozon(t *testing.T) {
	csv := "Дата начисления;Итого к начислению;Номер отправления\n" +
		"05.01.2026;980,25;OZ-999\n"

	txs, err := ParseMarketplaceCSV(strings.NewReader(csv))
	if err != nil {
		t.Fatalf("ParseMarketplaceCSV (Ozon): %v", err)
	}
	if len(txs) != 1 {
		t.Fatalf("ожидалась 1 транзакция, получено %d", len(txs))
	}
}
