package ingest

import (
	"os"
	"strings"
	"testing"
)

func TestParseCSV_SberFormat(t *testing.T) {
	csv := "Дата операции;Приход;Расход;Контрагент;ИНН контрагента;Назначение платежа\n" +
		"05.01.2026;;180000,00;Арендодатель ЗАО;7712345678;Аренда офиса\n" +
		"10.01.2026;250000,00;;Клиент ООО Ромашка;7723009845;Оплата по договору\n"

	txs, err := ParseCSV(strings.NewReader(csv))
	if err != nil {
		t.Fatalf("ParseCSV (Sber): %v", err)
	}
	if len(txs) != 2 {
		t.Fatalf("ожидалось 2 транзакции, получено %d", len(txs))
	}
	if txs[0].Amount != 180000 {
		t.Errorf("ожидалась сумма 180000, получено %v", txs[0].Amount)
	}
}

func TestDetectBankFormat_Unknown(t *testing.T) {
	header := []string{"col1", "col2"}
	if m := detectBankFormat(header); m != nil {
		t.Error("для неизвестного формата маппинг должен быть nil")
	}
}

func TestParseFile_SberViaDispatcher(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "sber_*.csv")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	csvData := "Дата операции;Приход;Расход;Контрагент;ИНН контрагента;Назначение платежа\n" +
		"05.01.2026;;180000,00;Арендодатель ЗАО;7712345678;Аренда офиса\n"
	if _, err := tmpFile.WriteString(csvData); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	txs, err := ParseFile(tmpFile.Name())
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if len(txs) != 1 {
		t.Fatalf("ожидалась 1 транзакция, получено %d", len(txs))
	}
}

func TestDetectBankFormat_Alfa(t *testing.T) {
	header := []string{"дата проводки", "сумма по дебету", "сумма по кредиту", "наименование контрагента"}
	mapping := detectBankFormat(header)
	if mapping == nil {
		t.Fatal("ожидался маппинг Альфа-Банка, получен nil")
	}
	if mapping["дата операции"] != "дата проводки" {
		t.Errorf("неверный маппинг даты операции: %q", mapping["дата операции"])
	}
}

func TestDetectBankFormat_PSB(t *testing.T) {
	header := []string{"дата документа", "сумма платежа", "плательщик/получатель"}
	mapping := detectBankFormat(header)
	if mapping == nil {
		t.Fatal("ожидался маппинг ПСБ, получен nil")
	}
	if mapping["наименование контрагента"] != "плательщик/получатель" {
		t.Errorf("неверный маппинг контрагента: %q", mapping["наименование контрагента"])
	}
}
