package main

import (
	"encoding/json"
	"os"
	"sort"

	"github.com/wwpp/finaudit/internal/metrics"
	"github.com/wwpp/finaudit/internal/models"
)

// QuarterSummary — оборот дохода/расхода за квартал.
type QuarterSummary struct {
	Quarter int     `json:"quarter"`
	Income  float64 `json:"income"`
	Expense float64 `json:"expense"`
	Net     float64 `json:"net"`
}

// AnnualExpected — эталон годового отчёта (ground-truth).
type AnnualExpected struct {
	Persona       string           `json:"persona"`
	Year          int              `json:"year"`
	TotalIncome   float64          `json:"total_income"`
	TotalExpense  float64          `json:"total_expense"`
	NetCashFlow   float64          `json:"net_cash_flow"`
	Quarters      []QuarterSummary `json:"quarters"`
	HasCashGap    bool             `json:"has_cash_gap"`
	CashGapReason string           `json:"cash_gap_reason,omitempty"`
}

// buildAnnualExpected считает эталон по полному списку транзакций персоны.
func buildAnnualExpected(persona string, txs []models.Transaction) AnnualExpected {
	var quarters [4]QuarterSummary
	for q := 0; q < 4; q++ {
		quarters[q].Quarter = q + 1
	}

	var totalIncome, totalExpense float64
	for _, tx := range txs {
		q := quarterOf(tx.Date) - 1
		if tx.Direction == models.In {
			quarters[q].Income = round2(quarters[q].Income + tx.Amount)
			totalIncome = round2(totalIncome + tx.Amount)
		} else {
			quarters[q].Expense = round2(quarters[q].Expense + tx.Amount)
			totalExpense = round2(totalExpense + tx.Amount)
		}
	}
	for q := 0; q < 4; q++ {
		quarters[q].Net = round2(quarters[q].Income - quarters[q].Expense)
	}

	result := AnnualExpected{
		Persona:      persona,
		Year:         year,
		TotalIncome:  totalIncome,
		TotalExpense: totalExpense,
		NetCashFlow:  round2(totalIncome - totalExpense),
		Quarters:     quarters[:],
	}

	sorted := make([]models.Transaction, len(txs))
	copy(sorted, txs)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Date.Before(sorted[j].Date) })
	audit := metrics.ComputeAudit(sorted)
	if audit.CashGap != nil {
		result.HasCashGap = true
		result.CashGapReason = audit.CashGap.Reason
	}

	return result
}

func writeAnnualExpected(path string, a AnnualExpected) error {
	data, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
