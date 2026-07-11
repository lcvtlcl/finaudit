package models

import "time"

// TaxProfile — налоговый профиль пользователя, влияет на состав проверок.
// TaxRegime: "usn" | "npd" | "osno" (основное для логики checks/compliance).
// LegalForm: "ip" | "ooo" | "self_employed" (для UI/текстов).
type TaxProfile struct {
	TaxRegime string
	LegalForm string
}

type Direction string

type Activity string

const (
	In  Direction = "in"
	Out Direction = "out"
)

const (
	ActivityOperating Activity = "операционная"
	ActivityInvesting Activity = "инвестиционная"
	ActivityFinancing Activity = "финансовая"
)

// Transaction — нормализованная транзакция (единая схема после парсинга выписки).
type Transaction struct {
	ID           int64     `json:"id"`
	UploadID     int64     `json:"upload_id"`
	Date         time.Time `json:"date"`
	Amount       float64   `json:"amount"`
	Direction    Direction `json:"direction"`
	Counterparty string    `json:"counterparty"`
	INN          string    `json:"inn"`
	Purpose      string    `json:"purpose"`
	Category     string    `json:"category"`
	Activity     Activity  `json:"activity"`
}

// AuditResult — контракт между движком метрик (производит), фронтом и LLM (потребляют).
// Цифры заполняет код (metrics). Summary и Recommendations заполняет LLM-слой.
// Подробное описание всех полей: CONTRACT.md
type AuditResult struct {
	UploadID          int64   `json:"upload_id"`            // ID загруженного файла (0 = не сохранено в БД)
	TaxRegime         string  `json:"tax_regime,omitempty"` // налоговый режим пользователя (usn/npd/osno) — для чек-листа и промпта
	Period            Period  `json:"period"`
	OpeningBalance    float64 `json:"opening_balance"`
	ClosingBalance    float64 `json:"closing_balance"`
	TotalIncome       float64 `json:"total_income"`
	TotalExpense      float64 `json:"total_expense"`
	NetCashFlow       float64 `json:"net_cash_flow"`
	ExcludedTransfers float64 `json:"excluded_transfers"` // metrics1: сумма внутренних переводов/займов, исключённых из выручки/расходов
	NetRefunds        float64 `json:"net_refunds"`        // metrics2: сумма учтённых сторно/возвратов

	OperatingCashFlow float64 `json:"operating_cash_flow"`
	InvestingCashFlow float64 `json:"investing_cash_flow"`
	FinancingCashFlow float64 `json:"financing_cash_flow"`

	ExpenseStructure []ExpenseCategory `json:"expense_structure"`  // для пирога расходов
	CashFlow         []CashFlowPoint   `json:"cash_flow"`          // помесячно: столбики приход/расход
	BalanceSeries    []BalancePoint    `json:"balance_series"`     // по дням: линия баланса + красная зона
	CashGap          *CashGap          `json:"cash_gap,omitempty"` // nil = разрыва нет

	Alerts          []Alert          `json:"alerts"`
	Summary         string           `json:"summary"`              // <- LLM
	Recommendations []string         `json:"recommendations"`      // <- LLM
	Checks          []Check          `json:"checks"`               // аудиторский чек-лист (типовые ошибки МСБ)
	Forecast        *Forecast        `json:"forecast,omitempty"`   // прогноз баланса вперёд (nil = нет регулярных платежей)
	Compliance      []ComplianceFlag `json:"compliance,omitempty"` // признаки правового риска (детерминированно, с цитатами статей)
	Scenarios       []Scenario       `json:"scenarios,omitempty"`  // предлагаемые what-if сценарии (генерит код при наличии разрыва)
	Rating          *Rating          `json:"rating,omitempty"`     // балл финансового здоровья (детерминированно из метрик)
}

// Rating — балл финансового здоровья бизнеса (0..100, буква A..E). Считает КОД, не ИИ.
// Это индикатор здоровья, НЕ кредитный скоринг.
type Rating struct {
	Score     int      `json:"score"`
	Grade     string   `json:"grade"`
	Label     string   `json:"label"`
	Positives []string `json:"positives"`
	Negatives []string `json:"negatives"`
}

// SimAction — изменение для what-if симуляции (перенос/дробление платежа). Применяет КОД.
type SimAction struct {
	Kind      string  `json:"kind"`      // "move" | "split"
	MatchText string  `json:"matchText"` // назначение/контрагент платежа, который меняем
	Amount    float64 `json:"amount"`    // сумма платежа (для точного матча)
	Days      int     `json:"days"`      // на сколько дней перенести (move) / сдвинуть вторую часть (split)
}

// Scenario — предлагаемый what-if сценарий (кнопка «Симулировать» на дашборде).
type Scenario struct {
	Label  string    `json:"label"`
	Action SimAction `json:"action"`
}

// ComplianceSeverity — уровень правового риска.
type ComplianceSeverity string

const (
	ComplianceOK        ComplianceSeverity = "ok"
	ComplianceAttention ComplianceSeverity = "attention"
	ComplianceRisk      ComplianceSeverity = "risk"
)

// ComplianceFlag — признак правового риска. Решение принимает КОД по условию;
// Statute — ссылка на норму для аудируемости, не источник рассуждений. ИИ здесь не участвует.
type ComplianceFlag struct {
	Code           string             `json:"code"`
	Title          string             `json:"title"`
	Severity       ComplianceSeverity `json:"severity"`
	Detail         string             `json:"detail"`
	Statute        string             `json:"statute"`
	Recommendation string             `json:"recommendation,omitempty"`
}

// PlannedPayment — запланированный будущий платёж (заявка на выплату), вносит пользователь.
// Учитывается в прогнозе баланса вперёд и в платёжном календаре.
type PlannedPayment struct {
	ID        int64     `json:"id"`
	Date      time.Time `json:"date"`
	Amount    float64   `json:"amount"`
	Direction Direction `json:"direction"`
	Purpose   string    `json:"purpose"`
}

// RecurringPayment — регулярный платёж, найденный в истории (основа прогноза вперёд).
type RecurringPayment struct {
	Counterparty string    `json:"counterparty"`
	Category     string    `json:"category"`
	Direction    Direction `json:"direction"`
	AvgAmount    float64   `json:"avg_amount"`
	PeriodDays   int       `json:"period_days"` // средний интервал между платежами
	NextDate     time.Time `json:"next_date"`   // ожидаемая дата следующего платежа
	Occurrences  int       `json:"occurrences"` // сколько раз встретился в истории
}

// Forecast — прогноз баланса вперёд по регулярным платежам.
// Киллер-фича «предупреждаем о будущем»: считает код, не угадывает нейросеть.
type Forecast struct {
	HorizonDays int                `json:"horizon_days"`
	Series      []BalancePoint     `json:"series"`        // проекция баланса вперёд по дням
	Recurring   []RecurringPayment `json:"recurring"`     // найденные регулярные платежи
	Gap         *CashGap           `json:"gap,omitempty"` // прогнозный разрыв (nil = не предвидится)
}

// CheckStatus — результат авто-проверки.
type CheckStatus string

const (
	CheckOK      CheckStatus = "ok"
	CheckWarning CheckStatus = "warning"
	CheckDanger  CheckStatus = "danger"
)

// Check — одна проверка из аудиторского чек-листа (топ ошибок МСБ).
type Check struct {
	Code           string      `json:"code"`
	Title          string      `json:"title"`
	Status         CheckStatus `json:"status"`
	Detail         string      `json:"detail"`
	Recommendation string      `json:"recommendation,omitempty"`
}

type Period struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

// ExpenseCategory — строка структуры расходов (slice, а не map — стабильный порядок для UI).
type ExpenseCategory struct {
	Category string  `json:"category"`
	Amount   float64 `json:"amount"`
	Share    float64 `json:"share"` // доля 0..1 (для процентов на пироге)
}

// CashFlowPoint — агрегат за период (для столбиков прихода/расхода).
type CashFlowPoint struct {
	Period  string  `json:"period"` // "2026-06" или "2026-W26"
	Inflow  float64 `json:"inflow"`
	Outflow float64 `json:"outflow"`
	Balance float64 `json:"balance"` // баланс на конец периода
}

// BalancePoint — баланс на конкретный день (для линии баланса и подсветки разрыва).
type BalancePoint struct {
	Date    time.Time `json:"date"`
	Balance float64   `json:"balance"`
}

// CashGap — киллер-фича: прогноз кассового разрыва.
type CashGap struct {
	Date             time.Time `json:"date"`              // день, когда баланс уходит в минус
	ProjectedBalance float64   `json:"projected_balance"` // баланс в этот день (отрицательный)
	Shortfall        float64   `json:"shortfall"`         // размер нехватки (положительное число)
	Reason           string    `json:"reason"`            // почему: совпадение платежей и т.п.
}

type Severity string

const (
	SeverityInfo    Severity = "info"
	SeverityWarning Severity = "warning"
	SeverityDanger  Severity = "danger"
)

// Alert — найденная аномалия/риск (структурный, чтобы фронт красил по severity).
type Alert struct {
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
}

// CounterpartyCheck — входные данные для comp2: статус контрагента по ИНН,
// полученный извне (DaData/ЕГРЮЛ). Код правил в compliance принимает это как факт.
type CounterpartyCheck struct {
	INN              string
	Name             string
	State            string // ACTIVE | LIQUIDATING | LIQUIDATED | BANKRUPT
	MassAddress      bool
	Disqualified     bool
	RegistrationDate time.Time // нулевое значение = дата неизвестна
}
