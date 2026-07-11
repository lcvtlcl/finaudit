// Package export формирует выгрузку AuditResult в Excel (.xlsx).
// Чистый слой: на вход готовый AuditResult, на выход — байты файла.
package export

import (
	"fmt"

	"github.com/xuri/excelize/v2"

	"github.com/wwpp/finaudit/internal/models"
)

// ToExcel строит .xlsx из аудита: листы «Сводка», «Расходы», «Денежный поток», «Чек-лист».
func ToExcel(res models.AuditResult) ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()

	// ----- Лист «Сводка» -----
	const sum = "Сводка"
	f.SetSheetName("Sheet1", sum)
	f.SetColWidth(sum, "A", "A", 32)
	f.SetColWidth(sum, "B", "B", 22)

	rows := [][]any{
		{"Финансовый аудит", ""},
		{"Период", fmt.Sprintf("%s — %s", res.Period.From.Format("02.01.2006"), res.Period.To.Format("02.01.2006"))},
		{"Остаток на начало, ₽", res.OpeningBalance},
		{"Остаток на конец, ₽", res.ClosingBalance},
		{"Доходы, ₽", res.TotalIncome},
		{"Расходы, ₽", res.TotalExpense},
		{"Чистый поток, ₽", res.NetCashFlow},
		{"", ""},
		{"Денежный поток по видам (ПБУ 23/2011)", ""},
		{"Операционный, ₽", res.OperatingCashFlow},
		{"Инвестиционный, ₽", res.InvestingCashFlow},
		{"Финансовый, ₽", res.FinancingCashFlow},
	}
	if res.CashGap != nil {
		rows = append(rows,
			[]any{"", ""},
			[]any{"Кассовый разрыв", res.CashGap.Date.Format("02.01.2006")},
			[]any{"Не хватает, ₽", res.CashGap.Shortfall},
			[]any{"Причина", res.CashGap.Reason},
		)
	}
	if res.Summary != "" {
		rows = append(rows, []any{"", ""}, []any{"Выводы", res.Summary})
	}
	for _, rec := range res.Recommendations {
		rows = append(rows, []any{"Рекомендация", rec})
	}
	writeRows(f, sum, rows)
	f.SetCellStyle(sum, "A1", "A1", boldStyle(f))

	// ----- Лист «Расходы» -----
	const exp = "Расходы"
	f.NewSheet(exp)
	f.SetColWidth(exp, "A", "A", 28)
	f.SetColWidth(exp, "B", "C", 16)
	writeRows(f, exp, [][]any{{"Статья", "Сумма, ₽", "Доля, %"}})
	for i, e := range res.ExpenseStructure {
		row := i + 2
		f.SetCellValue(exp, cell("A", row), e.Category)
		f.SetCellValue(exp, cell("B", row), e.Amount)
		f.SetCellValue(exp, cell("C", row), fmt.Sprintf("%.0f", e.Share*100))
	}

	// ----- Лист «Денежный поток» -----
	const cf = "Денежный поток"
	f.NewSheet(cf)
	f.SetColWidth(cf, "A", "D", 16)
	writeRows(f, cf, [][]any{{"Период", "Приход, ₽", "Расход, ₽", "Баланс, ₽"}})
	for i, p := range res.CashFlow {
		row := i + 2
		f.SetCellValue(cf, cell("A", row), p.Period)
		f.SetCellValue(cf, cell("B", row), p.Inflow)
		f.SetCellValue(cf, cell("C", row), p.Outflow)
		f.SetCellValue(cf, cell("D", row), p.Balance)
	}

	// ----- Лист «Чек-лист» -----
	const chk = "Чек-лист"
	f.NewSheet(chk)
	f.SetColWidth(chk, "A", "A", 30)
	f.SetColWidth(chk, "B", "B", 10)
	f.SetColWidth(chk, "C", "D", 40)
	writeRows(f, chk, [][]any{{"Проверка", "Статус", "Детали", "Рекомендация"}})
	for i, c := range res.Checks {
		row := i + 2
		f.SetCellValue(chk, cell("A", row), c.Title)
		f.SetCellValue(chk, cell("B", row), statusLabel(c.Status))
		f.SetCellValue(chk, cell("C", row), c.Detail)
		f.SetCellValue(chk, cell("D", row), c.Recommendation)
	}

	f.SetActiveSheet(0)
	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, fmt.Errorf("сборка xlsx: %w", err)
	}
	return buf.Bytes(), nil
}

func writeRows(f *excelize.File, sheet string, rows [][]any) {
	for i, r := range rows {
		for j, v := range r {
			f.SetCellValue(sheet, cell(string(rune('A'+j)), i+1), v)
		}
	}
}

func cell(col string, row int) string { return fmt.Sprintf("%s%d", col, row) }

func statusLabel(s models.CheckStatus) string {
	switch s {
	case models.CheckDanger:
		return "✗"
	case models.CheckWarning:
		return "!"
	default:
		return "✓"
	}
}

func boldStyle(f *excelize.File) int {
	id, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Size: 13}})
	return id
}
