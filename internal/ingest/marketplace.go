package ingest

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/xuri/excelize/v2"

	"github.com/wwpp/finaudit/internal/models"
)

// detectMarketplace определяет площадку (WB/Ozon) по заголовку и возвращает
// колонки: дата, сумма к перечислению, назначение/номер заказа.
type marketplaceCols struct {
	dateCol, amountCol, orderCol string
	source                       string
}

func detectMarketplaceFormat(header []string) *marketplaceCols {
	idx := buildColumnIndex(header)

	if hasColumn(idx, "к перечислению продавцу") || hasColumn(idx, "дата продажи") {
		return &marketplaceCols{
			dateCol:   "дата продажи",
			amountCol: "к перечислению продавцу",
			orderCol:  "номер заказа",
			source:    "wildberries",
		}
	}
	if hasColumn(idx, "итого к начислению") || hasColumn(idx, "дата начисления") {
		return &marketplaceCols{
			dateCol:   "дата начисления",
			amountCol: "итого к начислению",
			orderCol:  "номер отправления",
			source:    "ozon",
		}
	}
	return nil
}

// ParseMarketplaceCSV парсит выгрузку выплат WB/Ozon из CSV.
func ParseMarketplaceCSV(r io.Reader) ([]models.Transaction, error) {
	buf := &bytes.Buffer{}
	if _, err := io.Copy(buf, r); err != nil {
		return nil, fmt.Errorf("ошибка чтения CSV маркетплейса: %w", err)
	}
	text, err := detectEncoding(buf.Bytes())
	if err != nil {
		return nil, err
	}

	reader := csv.NewReader(strings.NewReader(text))
	reader.Comma = ';'
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true

	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения CSV маркетплейса: %w", err)
	}
	if len(records) == 0 {
		return nil, errNoTransactions
	}

	cols := detectMarketplaceFormat(records[0])
	if cols == nil {
		return nil, fmt.Errorf("не распознан формат маркетплейса (не WB/Ozon)")
	}

	return buildMarketplaceTransactions(records[1:], buildColumnIndex(records[0]), cols)
}

// ParseMarketplaceXLSX парсит выгрузку выплат WB/Ozon из XLSX.
func ParseMarketplaceXLSX(path string) ([]models.Transaction, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, fmt.Errorf("не удалось открыть XLSX маркетплейса: %w", err)
	}
	defer f.Close()

	sheet := f.GetSheetName(0)
	rows, err := f.GetRows(sheet)
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения XLSX маркетплейса: %w", err)
	}
	if len(rows) == 0 {
		return nil, errNoTransactions
	}

	cols := detectMarketplaceFormat(rows[0])
	if cols == nil {
		return nil, fmt.Errorf("не распознан формат маркетплейса (не WB/Ozon)")
	}

	return buildMarketplaceTransactions(rows[1:], buildColumnIndex(rows[0]), cols)
}

func buildMarketplaceTransactions(rows [][]string, idx map[string]int, cols *marketplaceCols) ([]models.Transaction, error) {
	get := func(row []string, key string) string {
		i, ok := idx[key]
		if !ok || i >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[i])
	}

	var txs []models.Transaction
	for _, row := range rows {
		dateStr := get(row, cols.dateCol)
		date, err := parseDate(dateStr)
		if err != nil {
			continue
		}

		amountStr := get(row, cols.amountCol)
		if amountStr == "" {
			continue
		}
		amount, err := parseAmount(amountStr)
		if err != nil {
			continue
		}
		if amount == 0 {
			continue
		}

		orderNum := get(row, cols.orderCol)
		purpose := fmt.Sprintf("Выплата %s, заказ %s", strings.Title(cols.source), orderNum)
		if orderNum == "" {
			purpose = fmt.Sprintf("Выплата %s", strings.Title(cols.source))
		}

		direction := models.In
		if amount < 0 {
			direction = models.Out
		}

		txs = append(txs, models.Transaction{
			Date:         date,
			Amount:       math.Abs(amount),
			Direction:    direction,
			Counterparty: strings.Title(cols.source),
			Purpose:      purpose,
			Category:     "Выручка (маркетплейс)",
		})
	}

	if len(txs) == 0 {
		return nil, errNoTransactions
	}
	return txs, nil
}
