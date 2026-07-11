package simulate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/wwpp/finaudit/internal/categorize"
	"github.com/wwpp/finaudit/internal/ingest"
	"github.com/wwpp/finaudit/internal/metrics"
	"github.com/wwpp/finaudit/internal/models"
)

type expectedQuarter struct {
	Quarter int     `json:"quarter"`
	Income  float64 `json:"income"`
	Expense float64 `json:"expense"`
	Net     float64 `json:"net"`
}

type expectedAnnual struct {
	Persona      string            `json:"persona"`
	Year         int               `json:"year"`
	TotalIncome  float64           `json:"total_income"`
	TotalExpense float64           `json:"total_expense"`
	NetCashFlow  float64           `json:"net_cash_flow"`
	Quarters     []expectedQuarter `json:"quarters"`
	HasCashGap   bool              `json:"has_cash_gap"`
}

// tolerance — допуск на округление копеек при сложении по кварталам.
const tolerance = 1.0

var personas = []string{"ip_retail_usn6", "ooo_services_usn15", "ecom_marketplace"}

func TestSimAnnualReconciliation(t *testing.T) {
	baseDir := "../ingest/testdata/sim"

	for _, persona := range personas {
		persona := persona
		t.Run(persona, func(t *testing.T) {
			dir := filepath.Join(baseDir, persona)

			expectedPath := filepath.Join(dir, "expected_annual.json")
			raw, err := os.ReadFile(expectedPath)
			if err != nil {
				t.Fatalf("не удалось прочитать эталон %s: %v", expectedPath, err)
			}
			var expected expectedAnnual
			if err := json.Unmarshal(raw, &expected); err != nil {
				t.Fatalf("не удалось распарсить эталон: %v", err)
			}

			var allTxs []models.Transaction
			var actualQuarters []expectedQuarter
			var totalIncome, totalExpense float64

			for q := 1; q <= 4; q++ {
				qPath := filepath.Join(dir, fmt.Sprintf("q%d.txt", q))
				txs, err := ingest.ParseFile(qPath)
				if err != nil {
					t.Fatalf("q%d: ошибка парсинга %s: %v", q, qPath, err)
				}
				txs = categorize.Categorize(txs)
				res := metrics.ComputeAudit(txs)

				actualQuarters = append(actualQuarters, expectedQuarter{
					Quarter: q,
					Income:  res.TotalIncome,
					Expense: res.TotalExpense,
					Net:     res.NetCashFlow,
				})
				totalIncome += res.TotalIncome
				totalExpense += res.TotalExpense

				allTxs = append(allTxs, txs...)
			}

			// сверка по кварталам
			for i, aq := range actualQuarters {
				eq := expected.Quarters[i]
				if diff := aq.Income - eq.Income; diff > tolerance || diff < -tolerance {
					t.Errorf("Q%d income: ожидалось %.2f, получено %.2f (расхождение %.2f)", eq.Quarter, eq.Income, aq.Income, diff)
				}
				if diff := aq.Expense - eq.Expense; diff > tolerance || diff < -tolerance {
					t.Errorf("Q%d expense: ожидалось %.2f, получено %.2f (расхождение %.2f)", eq.Quarter, eq.Expense, aq.Expense, diff)
				}
				if diff := aq.Net - eq.Net; diff > tolerance || diff < -tolerance {
					t.Errorf("Q%d net: ожидалось %.2f, получено %.2f (расхождение %.2f)", eq.Quarter, eq.Net, aq.Net, diff)
				}
			}

			// сверка годовых итогов
			netCashFlow := totalIncome - totalExpense
			if diff := totalIncome - expected.TotalIncome; diff > tolerance || diff < -tolerance {
				t.Errorf("годовой income: ожидалось %.2f, получено %.2f (расхождение %.2f)", expected.TotalIncome, totalIncome, diff)
			}
			if diff := totalExpense - expected.TotalExpense; diff > tolerance || diff < -tolerance {
				t.Errorf("годовой expense: ожидалось %.2f, получено %.2f (расхождение %.2f)", expected.TotalExpense, totalExpense, diff)
			}
			if diff := netCashFlow - expected.NetCashFlow; diff > tolerance || diff < -tolerance {
				t.Errorf("годовой net_cash_flow: ожидалось %.2f, получено %.2f (расхождение %.2f)", expected.NetCashFlow, netCashFlow, diff)
			}

			// кассовый разрыв — на объединённом годовом потоке транзакций
			sort.Slice(allTxs, func(i, j int) bool { return allTxs[i].Date.Before(allTxs[j].Date) })
			yearRes := metrics.ComputeAudit(allTxs)
			hasGap := yearRes.CashGap != nil
			if hasGap != expected.HasCashGap {
				t.Errorf("has_cash_gap: ожидалось %v, получено %v", expected.HasCashGap, hasGap)
			}

			t.Logf("%s: income=%.2f (эталон %.2f), expense=%.2f (эталон %.2f), net=%.2f (эталон %.2f), gap=%v (эталон %v)",
				persona, totalIncome, expected.TotalIncome, totalExpense, expected.TotalExpense, netCashFlow, expected.NetCashFlow, hasGap, expected.HasCashGap)
		})
	}
}
