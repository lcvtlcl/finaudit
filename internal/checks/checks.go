// Package checks — аудиторский чек-лист: прогоняет AuditResult по типовым
// финансовым ошибкам МСБ и возвращает структурный «светофор».
// Чистый слой: без БД, сети и LLM. Цифры уже посчитаны, тут только правила.
package checks

import (
	"fmt"
	"strings"

	"github.com/wwpp/finaudit/internal/models"
)

// Run выполняет все проверки над посчитанным аудитом с учётом налогового профиля.
func Run(res models.AuditResult, txs []models.Transaction, profile models.TaxProfile) []models.Check {
	checks := []models.Check{
		checkCashGap(res),
		checkOperatingHealth(res),
		checkProfitVsCash(res),
		checkTaxes(txs),
		checkPersonalMix(txs),
		checkReserve(res),
		checkConcentration(res),
		checkNetNegative(res),
	}

	switch profile.TaxRegime {
	case "npd":
		checks = append(checks, checkNPDContext(res))
	case "osno":
		// Для ОСНО не показываем УСН/NPD-специфику.
	default:
		// usn и пустое значение по умолчанию.
		checks = append(checks, checkTaxReserve(res, txs), checkVATThreshold(res))
	}

	return checks
}

func checkCashGap(res models.AuditResult) models.Check {
	c := models.Check{Code: "CASH_GAP", Title: "Кассовый разрыв"}
	if res.CashGap != nil {
		c.Status = models.CheckDanger
		c.Detail = fmt.Sprintf("Баланс уходит в минус %s, не хватает %.0f ₽.",
			res.CashGap.Date.Format("02.01.2006"), res.CashGap.Shortfall)
		c.Recommendation = "Перенесите крупные платежи или подготовьте резерв к этой дате."
		return c
	}
	c.Status = models.CheckOK
	c.Detail = "Баланс не уходит в минус за период."
	return c
}

func checkOperatingHealth(res models.AuditResult) models.Check {
	c := models.Check{Code: "OPERATING_HEALTH", Title: "Здоровье операционной деятельности"}
	if res.OperatingCashFlow >= 0 {
		c.Status = models.CheckOK
		c.Detail = fmt.Sprintf("Операционный поток положительный (%.0f ₽) — основной бизнес прибыльный.", res.OperatingCashFlow)
		if res.InvestingCashFlow < 0 && res.NetCashFlow < 0 {
			c.Recommendation = "Минус по итогу — из-за инвестиций, не операционки. Рассмотрите лизинг/рассрочку крупных покупок."
		}
		return c
	}
	c.Status = models.CheckWarning
	c.Detail = fmt.Sprintf("Операционный поток отрицательный (%.0f ₽) — основная деятельность убыточна.", res.OperatingCashFlow)
	c.Recommendation = "Проблема в операционке: пересмотрите цены и постоянные расходы, а не разовые траты."
	return c
}

func checkProfitVsCash(res models.AuditResult) models.Check {
	c := models.Check{Code: "PROFIT_VS_CASH", Title: "Прибыль vs живые деньги"}
	if res.OperatingCashFlow > 0 && res.ClosingBalance < res.OpeningBalance {
		c.Status = models.CheckWarning
		c.Detail = "Операционно прибыльны, но денег на счёте стало меньше — часть кассы где-то заморожена."
		c.Recommendation = "Проверьте дебиторку и склад: прибыль на бумаге ≠ деньги на счёте."
		return c
	}
	c.Status = models.CheckOK
	c.Detail = "Прибыль и остаток на счёте согласованы."
	return c
}

func checkTaxes(txs []models.Transaction) models.Check {
	c := models.Check{Code: "TAXES", Title: "Налоги и резерв под них"}
	var taxSum float64
	for _, tx := range txs {
		if tx.Direction != models.Out {
			continue
		}
		if strings.Contains(strings.ToLower(tx.Category), "налог") ||
			strings.Contains(strings.ToLower(tx.Purpose), "налог") {
			taxSum += tx.Amount
		}
	}
	if taxSum == 0 {
		c.Status = models.CheckWarning
		c.Detail = "Налоговых платежей в выписке не найдено."
		c.Recommendation = "Убедитесь, что налоги и взносы учтены — частая причина внезапного кассового разрыва."
		return c
	}
	c.Status = models.CheckOK
	c.Detail = fmt.Sprintf("Налоговые платежи учтены: %.0f ₽ за период.", taxSum)
	return c
}

// sumTaxPayments суммирует исходящие платежи, похожие на налоги/взносы.
func sumTaxPayments(txs []models.Transaction) float64 {
	var s float64
	for _, tx := range txs {
		if tx.Direction != models.Out {
			continue
		}
		if strings.Contains(strings.ToLower(tx.Category), "налог") ||
			strings.Contains(strings.ToLower(tx.Purpose), "налог") {
			s += tx.Amount
		}
	}
	return s
}

// checkTaxReserve оценивает налог УСН за период и проверяет, отложен ли резерв.
// Ставки: 6% «доходы» и 15% «доходы−расходы» (базовые ставки УСН 2026).
func checkTaxReserve(res models.AuditResult, txs []models.Transaction) models.Check {
	c := models.Check{Code: "TAX_RESERVE", Title: "Резерв под налог УСН"}
	if res.TotalIncome <= 0 {
		c.Status = models.CheckOK
		c.Detail = "Доходов за период нет — налог УСН к уплате не начисляется."
		return c
	}
	tax6 := 0.06 * res.TotalIncome
	base15 := res.TotalIncome - res.TotalExpense
	if base15 < 0 {
		base15 = 0
	}
	tax15 := 0.15 * base15
	paid := sumTaxPayments(txs)

	est := fmt.Sprintf("Ориентир по налогу за период: УСН 6%% «доходы» ≈ %.0f ₽; УСН 15%% «доходы−расходы» ≈ %.0f ₽.", tax6, tax15)
	if paid <= 0 {
		c.Status = models.CheckWarning
		c.Detail = est + " В выписке уплаты налога не видно."
		c.Recommendation = "Отложите резерв под налог заранее — трата «налоговых» денег частая причина кассового разрыва."
		return c
	}
	c.Status = models.CheckOK
	c.Detail = fmt.Sprintf("%s Уплачено налогов: %.0f ₽.", est, paid)
	return c
}

// checkVATThreshold предупреждает о приближении к порогу НДС для УСН.
// С 2026: при годовом доходе свыше 20 млн ₽ упрощенец становится плательщиком НДС (ставка 5%).
func checkVATThreshold(res models.AuditResult) models.Check {
	c := models.Check{Code: "VAT_THRESHOLD", Title: "Порог НДС (УСН, 2026)"}
	const threshold = 20_000_000.0
	if res.TotalIncome <= 0 {
		c.Status = models.CheckOK
		c.Detail = "Доходов за период нет."
		return c
	}
	days := res.Period.To.Sub(res.Period.From).Hours() / 24
	if days < 1 {
		days = 1
	}
	annualized := res.TotalIncome / days * 365

	switch {
	case annualized >= threshold:
		c.Status = models.CheckDanger
		c.Detail = fmt.Sprintf("Годовой доход (оценка ~%.1f млн ₽) превышает порог НДС 20 млн ₽.", annualized/1e6)
		c.Recommendation = "Готовьтесь платить НДС по ставке 5% и заложите его в цены."
	case annualized >= 0.8*threshold:
		c.Status = models.CheckWarning
		c.Detail = fmt.Sprintf("Годовой доход (оценка ~%.1f млн ₽) приближается к порогу НДС 20 млн ₽.", annualized/1e6)
		c.Recommendation = "За порогом — обязанность платить НДС 5%. Планируйте цены и учёт заранее."
	default:
		c.Status = models.CheckOK
		c.Detail = fmt.Sprintf("Годовой доход (оценка ~%.1f млн ₽) — ниже порога НДС 20 млн ₽.", annualized/1e6)
	}
	return c
}

func checkNPDContext(res models.AuditResult) models.Check {
	c := models.Check{Code: "NPD_CONTEXT", Title: "Контекст НПД"}
	const limit = 2_400_000.0
	if res.TotalIncome <= 0 {
		c.Status = models.CheckOK
		c.Detail = "Доходов за период нет. Для НПД ориентир — лимит 2,4 млн ₽ в год, ставки 4% с физлиц и 6% с юрлиц/ИП."
		return c
	}
	days := res.Period.To.Sub(res.Period.From).Hours() / 24
	if days < 1 {
		days = 1
	}
	annualized := res.TotalIncome / days * 365

	switch {
	case annualized >= limit:
		c.Status = models.CheckDanger
		c.Detail = fmt.Sprintf("Годовой доход (оценка ~%.2f млн ₽) превышает лимит НПД 2,4 млн ₽.", annualized/1e6)
		c.Recommendation = "При таком уровне дохода НПД неприменим: проверьте переход на другой режим. Ставки НПД: 4% с физлиц и 6% с юрлиц/ИП."
	case annualized >= 0.8*limit:
		c.Status = models.CheckWarning
		c.Detail = fmt.Sprintf("Годовой доход (оценка ~%.2f млн ₽) приближается к лимиту НПД 2,4 млн ₽.", annualized/1e6)
		c.Recommendation = "Контролируйте выручку: лимит НПД — 2,4 млн ₽ в год. Ставки НПД: 4% с физлиц и 6% с юрлиц/ИП."
	default:
		c.Status = models.CheckOK
		c.Detail = fmt.Sprintf("Годовой доход (оценка ~%.2f млн ₽) укладывается в лимит НПД 2,4 млн ₽. Ставки НПД: 4%% с физлиц и 6%% с юрлиц/ИП.", annualized/1e6)
	}
	return c
}

func checkPersonalMix(txs []models.Transaction) models.Check {
	c := models.Check{Code: "PERSONAL_MIX", Title: "Смешение личных и бизнес-средств"}
	keys := []string{"личн", "подотчёт", "подотчет", "вывод средств", "на карту", "перевод физ"}
	var sum float64
	for _, tx := range txs {
		if tx.Direction != models.Out {
			continue
		}
		p := strings.ToLower(tx.Purpose)
		for _, k := range keys {
			if strings.Contains(p, k) {
				sum += tx.Amount
				break
			}
		}
	}
	if sum > 0 {
		c.Status = models.CheckWarning
		c.Detail = fmt.Sprintf("Похоже на личные траты из оборотки: ~%.0f ₽.", sum)
		c.Recommendation = "Разделяйте личные и бизнес-финансы — иначе теряется реальная картина."
		return c
	}
	c.Status = models.CheckOK
	c.Detail = "Явного смешения личных и бизнес-средств не видно."
	return c
}

func checkReserve(res models.AuditResult) models.Check {
	c := models.Check{Code: "RESERVE", Title: "Финансовая подушка"}
	months := len(res.CashFlow)
	if months == 0 {
		months = 1
	}
	avgMonthlyExpense := res.TotalExpense / float64(months)
	minBal := res.OpeningBalance
	for _, p := range res.BalanceSeries {
		if p.Balance < minBal {
			minBal = p.Balance
		}
	}
	if avgMonthlyExpense > 0 && minBal < avgMonthlyExpense*0.1 {
		c.Status = models.CheckWarning
		c.Detail = fmt.Sprintf("Минимальный остаток за период (%.0f ₽) ниже 10%% месячных расходов.", minBal)
		c.Recommendation = "Заведите резерв хотя бы на 1 месяц расходов, чтобы пережить просадки."
		return c
	}
	c.Status = models.CheckOK
	c.Detail = "Остаток держится на безопасном уровне."
	return c
}

func checkConcentration(res models.AuditResult) models.Check {
	c := models.Check{Code: "CONCENTRATION", Title: "Концентрация расходов"}
	if len(res.ExpenseStructure) > 0 {
		top := res.ExpenseStructure[0]
		if top.Share > 0.4 {
			c.Status = models.CheckWarning
			c.Detail = fmt.Sprintf("Статья «%s» = %.0f%% всех расходов.", top.Category, top.Share*100)
			c.Recommendation = "Высокая зависимость от одной статьи — риск при её росте."
			return c
		}
	}
	c.Status = models.CheckOK
	c.Detail = "Расходы распределены без сильной концентрации."
	return c
}

func checkNetNegative(res models.AuditResult) models.Check {
	c := models.Check{Code: "NET_NEGATIVE", Title: "Доходы покрывают расходы"}
	if res.NetCashFlow < 0 {
		c.Status = models.CheckWarning
		c.Detail = fmt.Sprintf("За период расходы превысили доходы на %.0f ₽.", -res.NetCashFlow)
		c.Recommendation = "Следите, чтобы поток не уходил в минус систематически."
		return c
	}
	c.Status = models.CheckOK
	c.Detail = "Доходы за период покрывают расходы."
	return c
}
