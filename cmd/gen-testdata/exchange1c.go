package main

import (
	"fmt"
	"strings"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"

	"github.com/wwpp/finaudit/internal/models"
)

// renderClientBankExchange сериализует транзакции в формат 1CClientBankExchange (.txt),
// в кодировке Windows-1251 (как ожидает parseClientBankExchange).
func renderClientBankExchangeBytes(txs []models.Transaction, account string) ([]byte, error) {
	var b strings.Builder
	b.WriteString("1CClientBankExchange\n")
	b.WriteString("ВерсияФормата=1.03\n")
	b.WriteString("Кодировка=Windows\n")
	b.WriteString(fmt.Sprintf("РасчСчет=%s\n", account))
	b.WriteString("СекцияРасчСчет\n")
	b.WriteString(fmt.Sprintf("РасчСчет=%s\n", account))
	b.WriteString("КонецРасчСчет\n")

	id := 1000
	for _, tx := range txs {
		id++

		payer := "Наша организация"
		payerAcc := account
		payerINN := "0000000000"
		receiver := tx.Counterparty
		receiverAcc := "40702810000000099999"
		receiverINN := tx.INN

		if tx.Direction == models.In {
			payer = tx.Counterparty
			payerAcc = "40702810000000099999"
			payerINN = tx.INN
			receiver = "Наша организация"
			receiverAcc = account
			receiverINN = "0000000000"
		}

		b.WriteString("СекцияДокумент=Платежное поручение\n")
		b.WriteString(fmt.Sprintf("Номер=%d\n", id))
		b.WriteString(fmt.Sprintf("Дата=%s\n", tx.Date.Format("02.01.2006")))
		b.WriteString(fmt.Sprintf("Сумма=%.2f\n", tx.Amount))
		b.WriteString(fmt.Sprintf("Плательщик=%s\n", payer))
		b.WriteString(fmt.Sprintf("ПлательщикСчет=%s\n", payerAcc))
		b.WriteString(fmt.Sprintf("ПлательщикИНН=%s\n", payerINN))
		b.WriteString(fmt.Sprintf("Получатель=%s\n", receiver))
		b.WriteString(fmt.Sprintf("ПолучательСчет=%s\n", receiverAcc))
		b.WriteString(fmt.Sprintf("ПолучательИНН=%s\n", receiverINN))
		b.WriteString(fmt.Sprintf("НазначениеПлатежа=%s\n", tx.Purpose))
		b.WriteString("КонецДокумента\n")
	}

	b.WriteString("КонецФайла\n")

	enc := encoding.ReplaceUnsupported(charmap.Windows1251.NewEncoder())
	encoded, err := enc.String(b.String())
	if err != nil {
		return nil, err
	}
	return []byte(encoded), nil
}
