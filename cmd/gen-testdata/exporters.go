package main

import (
	"encoding/csv"
	"os"
	"strconv"

	"github.com/wwpp/finaudit/internal/models"
	"github.com/xuri/excelize/v2"
)

var csvHeader = []string{"date", "amount", "direction", "counterparty", "inn", "purpose", "category", "activity"}

func txToRow(tx models.Transaction) []string {
	return []string{
		tx.Date.Format("2006-01-02"),
		strconv.FormatFloat(tx.Amount, 'f', 2, 64),
		string(tx.Direction),
		tx.Counterparty,
		tx.INN,
		tx.Purpose,
		tx.Category,
		string(tx.Activity),
	}
}

// writeCSV пишет транзакции в CSV-файл.
func writeCSV(path string, txs []models.Transaction) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	if err := w.Write(csvHeader); err != nil {
		return err
	}
	for _, tx := range txs {
		if err := w.Write(txToRow(tx)); err != nil {
			return err
		}
	}
	return w.Error()
}

// writeXLSX пишет транзакции в XLSX-файл.
func writeXLSX(path string, txs []models.Transaction) error {
	f := excelize.NewFile()
	sheet := "Transactions"
	f.SetSheetName("Sheet1", sheet)

	for i, h := range csvHeader {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue(sheet, cell, h)
	}

	for r, tx := range txs {
		row := txToRow(tx)
		for c, v := range row {
			cell, _ := excelize.CoordinatesToCellName(c+1, r+2)
			f.SetCellValue(sheet, cell, v)
		}
	}

	return f.SaveAs(path)
}

// writeTxt пишет транзакции в формате 1С-обмена.
func writeTxt(path string, txs []models.Transaction, account string) error {
	content, err := renderClientBankExchangeBytes(txs, account)
	if err != nil {
		return err
	}
	return os.WriteFile(path, content, 0644)
}
