package ingest

import "testing"

func TestParseUPDXML_MinimalAttrs(t *testing.T) {
	raw := []byte(`<?xml version="1.0" encoding="utf-8"?>
<Файл>
  <Документ КНД="1115131">
    <СвСчФакт НомерСчФ="123" ДатаСчФ="10.07.2026" ВсегоОпл="15000.50"/>
    <СвПрод ИННЮЛ="7701234567" НаимОрг="ООО Ромашка"/>
  </Документ>
</Файл>`)

	doc, err := ParseUPDXML(raw)
	if err != nil {
		t.Fatalf("ParseUPDXML returned error: %v", err)
	}
	if doc.Number != "123" {
		t.Fatalf("Number = %q, want %q", doc.Number, "123")
	}
	if doc.Date != "10.07.2026" {
		t.Fatalf("Date = %q, want %q", doc.Date, "10.07.2026")
	}
	if doc.Amount != 15000.50 {
		t.Fatalf("Amount = %v, want 15000.50", doc.Amount)
	}
	if doc.Counterparty != "ООО Ромашка" {
		t.Fatalf("Counterparty = %q, want %q", doc.Counterparty, "ООО Ромашка")
	}
	if doc.INN != "7701234567" {
		t.Fatalf("INN = %q, want %q", doc.INN, "7701234567")
	}
}

func TestParseUPDXML_CommaAmount(t *testing.T) {
	raw := []byte(`<?xml version="1.0" encoding="utf-8"?>
<Файл>
  <Документ>
    <ТаблСчФакт ВсегоОпл="999,99"/>
    <СвПрод ИННЮЛ="7700000000" НаимОрг="ООО Тест"/>
  </Документ>
</Файл>`)

	doc, err := ParseUPDXML(raw)
	if err != nil {
		t.Fatalf("ParseUPDXML returned error: %v", err)
	}
	if doc.Amount != 999.99 {
		t.Fatalf("Amount = %v, want 999.99", doc.Amount)
	}
}

func TestParseUPDXML_Empty(t *testing.T) {
	_, err := ParseUPDXML(nil)
	if err == nil {
		t.Fatal("expected error for empty xml")
	}
}
