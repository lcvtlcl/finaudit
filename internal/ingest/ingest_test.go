package ingest

import (
	"bytes"
	"strings"
	"testing"

	"golang.org/x/text/encoding/charmap"

	"github.com/wwpp/finaudit/internal/models"
)

func TestParseBankStatementCSV(t *testing.T) {
	transactions, err := ParseFile("../../testdata/bank_statement_q2_2026.csv")
	if err != nil {
		t.Fatalf("ошибка парсинга bank_statement CSV: %v", err)
	}

	if len(transactions) == 0 {
		t.Fatal("не распарсилась ни одна транзакция")
	}

	// Первая строка — поступление 01.04.2026
	first := transactions[0]
	if first.Date.Format("2006-01-02") != "2026-04-01" {
		t.Errorf("первая транзакция: ожидаемая дата 2026-04-01, получена %s", first.Date.Format("2006-01-02"))
	}
	if first.Direction != models.In {
		t.Errorf("первая транзакция: ожидаемое направление in, получено %s", first.Direction)
	}
	if first.Counterparty != "ПАО ЭнергоСбыт" {
		t.Errorf("первая транзакция: ожидаемый контрагент 'ПАО ЭнергоСбыт', получено '%s'", first.Counterparty)
	}
	if first.Category == "" {
		t.Error("первая транзакция: категория не должна быть пустой")
	}

	// Проверяем, что есть расходные транзакции
	foundOut := false
	for _, tx := range transactions {
		if tx.Direction == models.Out {
			foundOut = true
			break
		}
	}
	if !foundOut {
		t.Error("не найдено ни одной расходной транзакции")
	}
}

func TestParseCSV(t *testing.T) {
	transactions, err := ParseFile("../../testdata/statement_q2_2026.csv")
	if err != nil {
		t.Fatalf("ошибка парсинга CSV: %v", err)
	}

	if len(transactions) != 73 {
		t.Errorf("ожидалось 73 транзакции, получено %d", len(transactions))
	}

	first := transactions[0]
	if first.Date.Format("2006-01-02") != "2026-04-01" {
		t.Errorf("первая транзакция: ожидаемая дата 2026-04-01, получена %s", first.Date.Format("2006-01-02"))
	}
	if first.Direction != models.In {
		t.Errorf("первая транзакция: ожидаемое направление in, получено %s", first.Direction)
	}
	if first.Amount != 150000.0 {
		t.Errorf("первая транзакция: ожидаемая сумма 150000, получена %f", first.Amount)
	}
	if first.Counterparty != "Входящий остаток" {
		t.Errorf("первая транзакция: ожидаемый контрагент 'Входящий остаток', получено '%s'", first.Counterparty)
	}
	if first.INN != "" {
		t.Errorf("первая транзакция: ожидаемый пустой ИНН, получено '%s'", first.INN)
	}
	if first.Purpose == "" || !strings.Contains(strings.ToLower(first.Purpose), "остаток") {
		t.Errorf("первая транзакция: Purpose должен содержать 'остаток', получено '%s'", first.Purpose)
	}

	foundExpense := false
	for _, tx := range transactions {
		if strings.Contains(tx.Counterparty, "Арендодатель") && tx.Amount == 80000.0 && tx.Direction == models.Out {
			foundExpense = true
			break
		}
	}
	if !foundExpense {
		t.Errorf("не найдена ожидаемая расходная транзакция с аренду")
	}
}

func TestParseClientBankExchangeFile(t *testing.T) {
	transactions, err := ParseFile("../../testdata/statement_1c.txt")
	if err != nil {
		t.Fatalf("ошибка парсинга 1CClientBankExchange: %v", err)
	}
	if len(transactions) != 10 {
		t.Fatalf("ожидалось 10 транзакций, получено %d", len(transactions))
	}

	for i, tx := range transactions {
		if tx.Amount <= 0 {
			t.Fatalf("транзакция %d: сумма должна быть положительной, получено %.2f", i, tx.Amount)
		}
		if strings.TrimSpace(tx.Counterparty) == "" {
			t.Fatalf("транзакция %d: пустой контрагент", i)
		}
		if strings.TrimSpace(tx.INN) == "" {
			t.Fatalf("транзакция %d: пустой ИНН", i)
		}
		if strings.TrimSpace(tx.Purpose) == "" {
			t.Fatalf("транзакция %d: пустое назначение", i)
		}
	}

	if transactions[0].Direction != models.In {
		t.Fatalf("первая транзакция должна быть входящей, получено %s", transactions[0].Direction)
	}
	if transactions[1].Direction != models.Out {
		t.Fatalf("вторая транзакция должна быть расходной, получено %s", transactions[1].Direction)
	}

	foundUSN := false
	for _, tx := range transactions {
		if strings.Contains(strings.ToLower(tx.Purpose), "усн") {
			foundUSN = true
			if tx.Direction != models.Out {
				t.Fatalf("налог УСН должен быть расходом, получено %s", tx.Direction)
			}
		}
	}
	if !foundUSN {
		t.Fatal("не найдена транзакция с налогом УСН")
	}
}

func TestParseClientBankExchangeCP1251(t *testing.T) {
	source := "1CClientBankExchange\nКодировка=Windows\nРасчСчет=40702810900000012345\nСекцияДокумент=Платежное поручение\nДата=01.07.2026\nСумма=1000,00\nПлательщикСчет=40702810900000012345\nПлательщик=ООО ФинАудит\nПлательщикИНН=7704001122\nПолучательСчет=40702810900000099999\nПолучатель=ФНС России\nПолучательИНН=7701000000\nНазначениеПлатежа=Уплата налога УСН\nКонецДокумента\n"
	encoded, err := charmap.Windows1251.NewEncoder().Bytes([]byte(source))
	if err != nil {
		t.Fatalf("ошибка кодирования cp1251: %v", err)
	}

	transactions, err := parseClientBankExchange(bytes.Clone(encoded))
	if err != nil {
		t.Fatalf("ошибка парсинга cp1251: %v", err)
	}
	if len(transactions) != 1 {
		t.Fatalf("ожидалась 1 транзакция, получено %d", len(transactions))
	}
	if transactions[0].Counterparty != "ФНС России" {
		t.Fatalf("контрагент: %q, ожидалось 'ФНС России'", transactions[0].Counterparty)
	}
}
