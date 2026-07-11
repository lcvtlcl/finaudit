// Демо-команда: прогоняет весь конвейер ingest -> categorize -> metrics -> llm
// на файле выписки и печатает результат в консоль.
//
//	go run ./cmd/auditdemo                 # на testdata по умолчанию
//	go run ./cmd/auditdemo path/to.csv     # на своём файле
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/wwpp/finaudit/internal/categorize"
	"github.com/wwpp/finaudit/internal/checks"
	"github.com/wwpp/finaudit/internal/config"
	"github.com/wwpp/finaudit/internal/ingest"
	"github.com/wwpp/finaudit/internal/llm"
	"github.com/wwpp/finaudit/internal/metrics"
	"github.com/wwpp/finaudit/internal/models"
)

func main() {
	path := "testdata/statement_q2_2026.csv"
	if len(os.Args) > 1 {
		path = os.Args[1]
	}

	txs, err := ingest.ParseFile(path)
	if err != nil {
		fmt.Println("ошибка парсинга:", err)
		os.Exit(1)
	}
	txs = categorize.Categorize(txs)
	res := metrics.ComputeAudit(txs)
	res.Checks = checks.Run(res, txs, models.TaxProfile{TaxRegime: "usn"})

	fmt.Println("==================== ФИНАНСОВЫЙ АУДИТ ====================")
	fmt.Printf("Период:            %s — %s\n", res.Period.From.Format("02.01.2006"), res.Period.To.Format("02.01.2006"))
	fmt.Printf("Остаток на начало: %14.0f ₽\n", res.OpeningBalance)
	fmt.Printf("Остаток на конец:  %14.0f ₽\n", res.ClosingBalance)
	fmt.Printf("Доходы:            %14.0f ₽\n", res.TotalIncome)
	fmt.Printf("Расходы:           %14.0f ₽\n", res.TotalExpense)
	fmt.Printf("Чистый поток:      %14.0f ₽\n", res.NetCashFlow)
	fmt.Println("---- Денежный поток по видам деятельности (ПБУ 23/2011) ----")
	fmt.Printf("  Операционный:    %14.0f ₽\n", res.OperatingCashFlow)
	fmt.Printf("  Инвестиционный:  %14.0f ₽\n", res.InvestingCashFlow)
	fmt.Printf("  Финансовый:      %14.0f ₽\n", res.FinancingCashFlow)
	fmt.Println("---- Структура расходов ----")
	for _, e := range res.ExpenseStructure {
		fmt.Printf("  %-22s %12.0f ₽  (%.0f%%)\n", e.Category, e.Amount, e.Share*100)
	}
	if res.CashGap != nil {
		fmt.Println("---- ⚠ Кассовый разрыв ----")
		fmt.Printf("  %s: не хватает %.0f ₽\n  Причина: %s\n",
			res.CashGap.Date.Format("02.01.2006"), res.CashGap.Shortfall, res.CashGap.Reason)
	}
	fmt.Println("---- Алерты ----")
	for _, a := range res.Alerts {
		fmt.Printf("  [%s] %s\n", a.Severity, a.Message)
	}
	fmt.Println("---- Аудиторский чек-лист (топ ошибок МСБ) ----")
	for _, c := range res.Checks {
		mark := "✓"
		if c.Status == "warning" {
			mark = "!"
		} else if c.Status == "danger" {
			mark = "✗"
		}
		fmt.Printf("  [%s] %s — %s\n", mark, c.Title, c.Detail)
		if c.Recommendation != "" {
			fmt.Printf("        → %s\n", c.Recommendation)
		}
	}

	// LLM-слой: выводы по-человечески (если задан ключ DeepSeek)
	cfg, _ := config.Load()
	client := llm.New(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	fmt.Println("\n==================== ВЫВОДЫ (DeepSeek) ====================")
	if err := client.Enrich(ctx, &res); err != nil {
		fmt.Println("LLM пропущен:", err)
		return
	}
	fmt.Println(res.Summary)
	if len(res.Recommendations) > 0 {
		fmt.Println("\nРекомендации:")
		for _, r := range res.Recommendations {
			fmt.Printf("  • %s\n", r)
		}
	}
}
