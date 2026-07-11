// Стретч p9: парсер УПД (универсальный передаточный документ) в формате
// ФНС-XML. Источник документов для internal/reconcile (p7).
package ingest

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// UPDDocument — минимальный набор полей УПД, нужных для сверки с выпиской.
type UPDDocument struct {
	XMLName      xml.Name `xml:"Файл"`
	Number       string
	Date         string
	Amount       float64
	Counterparty string
	INN          string
}

type updAny struct {
	XMLName xml.Name
	Attrs   []xml.Attr `xml:",any,attr"`
	Nodes   []updAny   `xml:",any"`
	Text    string     `xml:",chardata"`
}

func ParseUPDXML(raw []byte) (UPDDocument, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return UPDDocument{}, errors.New("ParseUPDXML: empty input")
	}

	var root updAny
	if err := xml.Unmarshal(raw, &root); err != nil {
		return UPDDocument{}, fmt.Errorf("ParseUPDXML: unmarshal xml: %w", err)
	}

	doc := UPDDocument{XMLName: root.XMLName}

	// Номер/дата документа: ищем самые типичные атрибуты ФНС XML.
	doc.Number = firstAttrDeep(root,
		"НомерСчФ",
		"НомерДок",
	)
	doc.Date = firstAttrDeep(root,
		"ДатаСчФ",
		"ДатаДок",
		"ДатаИнфПр",
	)

	// Сумма: приоритетно "ВсегоОпл", затем "СумНалВсего", затем "СтТовБезНДСВсего".
	doc.Amount = firstFloatAttrDeep(root,
		"ВсегоОпл",
		"СумНалВсего",
		"СтТовБезНДСВсего",
	)

	// Контрагент/ИНН: для сверки нам обычно важнее внешняя сторона документа.
	// Ищем сначала продавца/исполнителя, потом покупателя как fallback.
	doc.Counterparty = firstAttrDeep(root,
		"НаимОрг",
		"НаимПолн",
		"НаимПрод",
		"НаимПок",
		"НаимЭконСубСост",
		"НаимДокОпр",
	)
	doc.INN = firstAttrDeep(root,
		"ИННЮЛ",
		"ИННФЛ",
		"ИННПрод",
		"ИННПок",
	)

	doc.Number = strings.TrimSpace(doc.Number)
	doc.Date = strings.TrimSpace(doc.Date)
	doc.Counterparty = strings.TrimSpace(doc.Counterparty)
	doc.INN = strings.TrimSpace(doc.INN)

	if doc.Number == "" && doc.Date == "" && doc.Amount == 0 && doc.Counterparty == "" && doc.INN == "" {
		return UPDDocument{}, errors.New("ParseUPDXML: no supported UPD fields found")
	}

	return doc, nil
}

func firstAttrDeep(n updAny, names ...string) string {
	nameSet := make(map[string]struct{}, len(names))
	for _, name := range names {
		nameSet[name] = struct{}{}
	}

	var walk func(updAny) string
	walk = func(cur updAny) string {
		for _, a := range cur.Attrs {
			if _, ok := nameSet[a.Name.Local]; ok {
				v := strings.TrimSpace(a.Value)
				if v != "" {
					return v
				}
			}
		}
		for _, child := range cur.Nodes {
			if v := walk(child); v != "" {
				return v
			}
		}
		return ""
	}

	return walk(n)
}

func firstFloatAttrDeep(n updAny, names ...string) float64 {
	s := firstAttrDeep(n, names...)
	if s == "" {
		return 0
	}
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, ",", ".")
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v
}
