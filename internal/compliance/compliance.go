// Package compliance — детерминированные проверки на признаки правового риска.
// Решение принимает КОД по условию на данных; ИИ здесь не участвует.
// Каждый флаг привязан к статье (Statute) для аудируемости — норма это ссылка, не источник рассуждений.
package compliance

import (
	"fmt"
	"strings"
	"time"

	"github.com/wwpp/finaudit/internal/models"
)

// Run прогоняет выписку по правилам и возвращает флаги правового риска.
func Run(res models.AuditResult, txs []models.Transaction, counterparties []models.CounterpartyCheck, profile models.TaxProfile) []models.ComplianceFlag {
	flags := []models.ComplianceFlag{
		cashWithdrawal(txs, res.TotalExpense),
		personalMixing(txs),
	}

	switch profile.TaxRegime {
	case "npd":
		flags = append(flags, npdContext(res))
	case "osno":
		// Для ОСНО не показываем УСН/NPD-специфику.
	default:
		flags = append(flags, vatThreshold(res), usnLimit(res))
	}

	flags = append(flags, counterpartyRisk(counterparties)...)
	return flags
}

// counterpartyRisk — comp2: базовые риск-флаги по статусу контрагента в ЕГРЮЛ/ЕГРИП.
// Данные по ИНН — внешние (DaData), решение принимает код по условию, не ИИ.
func counterpartyRisk(checks []models.CounterpartyCheck) []models.ComplianceFlag {
	var flags []models.ComplianceFlag
	const newCompanyThreshold = 180 * 24 * time.Hour // моложе ~6 месяцев

	for _, cp := range checks {
		label := cp.Name
		if label == "" {
			label = cp.INN
		}

		switch cp.State {
		case "LIQUIDATING":
			flags = append(flags, models.ComplianceFlag{
				Code:           "COUNTERPARTY_LIQUIDATING",
				Title:          "Контрагент в процессе ликвидации",
				Severity:       models.ComplianceRisk,
				Detail:         fmt.Sprintf("Контрагент %s (ИНН %s) находится в процессе ликвидации.", label, cp.INN),
				Statute:        StatuteText("PERSONAL_54_1", "НК РФ ст. 54.1 (должная осмотрительность)"),
				Recommendation: "Проверьте актуальность договора и наличие подтверждающих документов по операциям с этим контрагентом.",
			})
		case "LIQUIDATED":
			flags = append(flags, models.ComplianceFlag{
				Code:           "COUNTERPARTY_LIQUIDATED",
				Title:          "Контрагент ликвидирован",
				Severity:       models.ComplianceRisk,
				Detail:         fmt.Sprintf("Контрагент %s (ИНН %s) ликвидирован.", label, cp.INN),
				Statute:        StatuteText("PERSONAL_54_1", "НК РФ ст. 54.1 (должная осмотрительность)"),
				Recommendation: "Операции с ликвидированным контрагентом создают риск непризнания расходов при проверке — подготовьте обосновывающие документы.",
			})
		case "BANKRUPT":
			flags = append(flags, models.ComplianceFlag{
				Code:           "COUNTERPARTY_BANKRUPT",
				Title:          "Контрагент в процедуре банкротства",
				Severity:       models.ComplianceRisk,
				Detail:         fmt.Sprintf("Контрагент %s (ИНН %s) находится в процедуре банкротства.", label, cp.INN),
				Statute:        StatuteText("PERSONAL_54_1", "НК РФ ст. 54.1 (должная осмотрительность)"),
				Recommendation: "Проверьте риски невозврата/незакрытия обязательств по этому контрагенту.",
			})
		}

		if cp.State == "ACTIVE" && !cp.RegistrationDate.IsZero() && time.Since(cp.RegistrationDate) < newCompanyThreshold {
			flags = append(flags, models.ComplianceFlag{
				Code:           "COUNTERPARTY_NEW",
				Title:          "Контрагент недавно создан",
				Severity:       models.ComplianceAttention,
				Detail:         fmt.Sprintf("Контрагент %s (ИНН %s) зарегистрирован %s — менее 6 месяцев назад.", label, cp.INN, cp.RegistrationDate.Format("02.01.2006")),
				Statute:        StatuteText("PERSONAL_54_1", "НК РФ ст. 54.1 (должная осмотрительность)"),
				Recommendation: "Молодые компании чаще фигурируют в схемах с фирмами-однодневками — соберите подтверждающие документы по сделке.",
			})
		}

		if cp.MassAddress {
			flags = append(flags, models.ComplianceFlag{
				Code:           "COUNTERPARTY_MASS_ADDRESS",
				Title:          "Массовый адрес регистрации контрагента",
				Severity:       models.ComplianceAttention,
				Detail:         fmt.Sprintf("Контрагент %s (ИНН %s) зарегистрирован по адресу массовой регистрации.", label, cp.INN),
				Statute:        StatuteText("PERSONAL_54_1", "НК РФ ст. 54.1 (должная осмотрительность)"),
				Recommendation: "Признак технической компании — соберите дополнительные подтверждающие документы по сделке.",
			})
		}

		if cp.Disqualified {
			flags = append(flags, models.ComplianceFlag{
				Code:           "COUNTERPARTY_DISQUALIFIED",
				Title:          "Дисквалификация руководителя контрагента",
				Severity:       models.ComplianceRisk,
				Detail:         fmt.Sprintf("Руководитель контрагента %s (ИНН %s) дисквалифицирован.", label, cp.INN),
				Statute:        StatuteText("PERSONAL_54_1", "НК РФ ст. 54.1 (должная осмотрительность)"),
				Recommendation: "Сделки, подписанные дисквалифицированным лицом, могут быть признаны недействительными — проверьте полномочия подписанта.",
			})
		}
	}

	return flags
}

// annualizedIncome оценивает годовой доход по доходу за период.
func annualizedIncome(res models.AuditResult) float64 {
	if res.TotalIncome <= 0 {
		return 0
	}
	days := res.Period.To.Sub(res.Period.From).Hours() / 24
	if days < 1 {
		days = 1
	}
	return res.TotalIncome / days * 365
}

func vatThreshold(res models.AuditResult) models.ComplianceFlag {
	c := models.ComplianceFlag{Code: "VAT_THRESHOLD", Title: "Порог НДС для УСН",
		Statute: StatuteText("VAT_THRESHOLD", "НК РФ — порог освобождения от НДС для УСН (20 млн ₽ с 2026)")}
	const threshold = 20_000_000.0
	ann := annualizedIncome(res)
	switch {
	case ann <= 0:
		c.Severity = models.ComplianceOK
		c.Detail = "Доходов за период нет."
	case ann >= threshold:
		c.Severity = models.ComplianceRisk
		c.Detail = fmt.Sprintf("Годовой доход (оценка ~%.1f млн ₽) превышает порог 20 млн ₽ — возникает обязанность платить НДС.", ann/1e6)
		c.Recommendation = "Оформите переход на НДС (ставка 5%) и заложите его в цены."
	case ann >= 0.8*threshold:
		c.Severity = models.ComplianceAttention
		c.Detail = fmt.Sprintf("Годовой доход (оценка ~%.1f млн ₽) приближается к порогу 20 млн ₽.", ann/1e6)
		c.Recommendation = "За порогом появится обязанность по НДС — спланируйте заранее."
	default:
		c.Severity = models.ComplianceOK
		c.Detail = fmt.Sprintf("Годовой доход (оценка ~%.1f млн ₽) ниже порога НДС.", ann/1e6)
	}
	return c
}

func usnLimit(res models.AuditResult) models.ComplianceFlag {
	c := models.ComplianceFlag{Code: "USN_LIMIT", Title: "Лимит дохода на УСН", Statute: StatuteText("USN_LIMIT", "НК РФ ст. 346.13")}
	const limit = 490_500_000.0
	ann := annualizedIncome(res)
	if ann >= limit {
		c.Severity = models.ComplianceRisk
		c.Detail = fmt.Sprintf("Годовой доход (оценка ~%.0f млн ₽) у лимита УСН (490,5 млн ₽) — риск потери права на спецрежим.", ann/1e6)
		c.Recommendation = "Контролируйте доход; при превышении — переход на ОСНО."
		return c
	}
	c.Severity = models.ComplianceOK
	c.Detail = "Доход в пределах лимита УСН."
	return c
}

func npdContext(res models.AuditResult) models.ComplianceFlag {
	c := models.ComplianceFlag{
		Code:    "NPD_CONTEXT",
		Title:   "Лимит и условия НПД",
		Statute: "НПД: лимит 2,4 млн ₽ в год; ставки 4% с физлиц и 6% с юрлиц/ИП",
	}
	const limit = 2_400_000.0
	ann := annualizedIncome(res)
	switch {
	case ann <= 0:
		c.Severity = models.ComplianceOK
		c.Detail = "Доходов за период нет. Для НПД ориентир — лимит 2,4 млн ₽ в год, ставки 4%/6%."
	case ann >= limit:
		c.Severity = models.ComplianceRisk
		c.Detail = fmt.Sprintf("Годовой доход (оценка ~%.2f млн ₽) превышает лимит НПД 2,4 млн ₽.", ann/1e6)
		c.Recommendation = "НПД неприменим при таком доходе — проверьте переход на другой режим."
	case ann >= 0.8*limit:
		c.Severity = models.ComplianceAttention
		c.Detail = fmt.Sprintf("Годовой доход (оценка ~%.2f млн ₽) приближается к лимиту НПД 2,4 млн ₽.", ann/1e6)
		c.Recommendation = "Контролируйте выручку, чтобы не выйти за лимит НПД."
	default:
		c.Severity = models.ComplianceOK
		c.Detail = fmt.Sprintf("Годовой доход (оценка ~%.2f млн ₽) укладывается в лимит НПД 2,4 млн ₽.", ann/1e6)
	}
	return c
}

func cashWithdrawal(txs []models.Transaction, totalExpense float64) models.ComplianceFlag {
	c := models.ComplianceFlag{Code: "CASH_115FZ", Title: "Снятие наличных (115-ФЗ)",
		Statute: StatuteText("CASH_115FZ", "ФЗ-115; методички ЦБ 18-МР / 19-МР")}
	keys := []string{"снятие", "наличн", "выдача наличных", "банкомат"}
	var cash float64
	for _, tx := range txs {
		if tx.Direction != models.Out {
			continue
		}
		s := strings.ToLower(tx.Purpose + " " + tx.Counterparty + " " + tx.Category)
		for _, k := range keys {
			if strings.Contains(s, k) {
				cash += tx.Amount
				break
			}
		}
	}
	switch {
	case totalExpense > 0 && cash/totalExpense > 0.3:
		c.Severity = models.ComplianceRisk
		c.Detail = fmt.Sprintf("Снятие наличных ~%.0f ₽ = %.0f%% расходов — банк может расценить как признак обналичивания.", cash, cash/totalExpense*100)
		c.Recommendation = "Снижайте долю наличных, храните подтверждающие документы по тратам."
	case cash > 0:
		c.Severity = models.ComplianceAttention
		c.Detail = fmt.Sprintf("Снятие наличных ~%.0f ₽ — держите долю умеренной и документируйте расходы.", cash)
	default:
		c.Severity = models.ComplianceOK
		c.Detail = "Крупных снятий наличных не видно."
	}
	return c
}

func personalMixing(txs []models.Transaction) models.ComplianceFlag {
	c := models.ComplianceFlag{Code: "PERSONAL_54_1", Title: "Смешение личного и бизнеса",
		Statute: StatuteText("PERSONAL_54_1", "НК РФ ст. 54.1 (НДФЛ/взносы при выводе физлицам)")}
	keys := []string{"личн", "подотчёт", "подотчет", "на карту", "перевод физ", "вывод средств"}
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
		c.Severity = models.ComplianceAttention
		c.Detail = fmt.Sprintf("Переводы физлицам/на карту ~%.0f ₽ — риск переквалификации в доход физлица (НДФЛ + взносы).", sum)
		c.Recommendation = "Разделяйте личные и бизнес-финансы; по выплатам физлицам оформляйте основание."
		return c
	}
	c.Severity = models.ComplianceOK
	c.Detail = "Явного вывода средств на физлиц не видно."
	return c
}
