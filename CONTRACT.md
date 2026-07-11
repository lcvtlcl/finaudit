# Контракт `models.AuditResult`

Это шов между слоями: `metrics` производит, `llm` дополняет, `api` отдаёт, `web` потребляет.

> **Правило:** цифры считает только код (`metrics`). LLM заполняет только `summary` и `recommendations`.

---

## Пример JSON

```json
{
  "upload_id": 42,
  "period": { "from": "2026-04-01T00:00:00Z", "to": "2026-06-30T00:00:00Z" },
  "opening_balance": 150000.0,
  "closing_balance": 87500.0,
  "total_income": 980000.0,
  "total_expense": 1042500.0,
  "net_cash_flow": -62500.0,
  "operating_cash_flow": 45000.0,
  "investing_cash_flow": -107500.0,
  "financing_cash_flow": 0.0,
  "expense_structure": [
    { "category": "Аренда",  "amount": 240000.0, "share": 0.23 },
    { "category": "Зарплата", "amount": 380000.0, "share": 0.36 }
  ],
  "cash_flow": [
    { "period": "2026-04", "inflow": 320000.0, "outflow": 355000.0, "balance": 115000.0 }
  ],
  "balance_series": [
    { "date": "2026-04-01T00:00:00Z", "balance": 150000.0 },
    { "date": "2026-04-03T00:00:00Z", "balance": 121000.0 }
  ],
  "cash_gap": {
    "date": "2026-05-22T00:00:00Z",
    "projected_balance": -14000.0,
    "shortfall": 14000.0,
    "reason": "совпали крупные списания: «Аренда офиса» (80000 ₽) и «Налог УСН» (34000 ₽)"
  },
  "alerts": [
    { "severity": "danger",  "message": "Кассовый разрыв 22.05.2026: баланс уходит в минус, не хватает 14000 ₽" },
    { "severity": "warning", "message": "Расходы за период превышают доходы на 62500 ₽" },
    { "severity": "info",    "message": "Крупнейшая статья расходов: Зарплата (36% всех расходов)" }
  ],
  "summary": "Заполняет LLM. 2-4 предложения о финансовой картине.",
  "recommendations": [
    "Заполняет LLM. Практичный совет 1.",
    "Совет 2."
  ]
}
```

---

## Описание полей

| Поле | Тип | Кто заполняет | Описание |
|---|---|---|---|
| `upload_id` | int64 | api/storage | ID загрузки в БД; 0 если не сохранено |
| `period.from` | ISO8601 | metrics | Дата первой транзакции |
| `period.to` | ISO8601 | metrics | Дата последней транзакции |
| `opening_balance` | float64 | metrics | Входящий остаток (первая строка выписки) |
| `closing_balance` | float64 | metrics | Исходящий остаток = opening + net_cash_flow |
| `total_income` | float64 | metrics | Сумма всех поступлений |
| `total_expense` | float64 | metrics | Сумма всех списаний |
| `net_cash_flow` | float64 | metrics | total_income − total_expense |
| `operating_cash_flow` | float64 | metrics | Поток по операционной деятельности |
| `investing_cash_flow` | float64 | metrics | Поток по инвестиционной деятельности |
| `financing_cash_flow` | float64 | metrics | Поток по финансовой деятельности |
| `expense_structure[]` | array | metrics | Расходы по категориям, убывание по сумме |
| `expense_structure[].category` | string | metrics | Название категории (из CSV или контрагент) |
| `expense_structure[].amount` | float64 | metrics | Сумма расходов по категории |
| `expense_structure[].share` | float64 | metrics | Доля 0..1 от total_expense |
| `cash_flow[]` | array | metrics | Помесячный агрегат (для столбиков на дашборде) |
| `cash_flow[].period` | string | metrics | Месяц в формате `"ГГГГ-ММ"` |
| `cash_flow[].inflow` | float64 | metrics | Приход за месяц |
| `cash_flow[].outflow` | float64 | metrics | Расход за месяц |
| `cash_flow[].balance` | float64 | metrics | Баланс на конец месяца |
| `balance_series[]` | array | metrics | Дневная линия баланса (для графика) |
| `balance_series[].date` | ISO8601 | metrics | Дата точки |
| `balance_series[].balance` | float64 | metrics | Баланс на эту дату |
| `cash_gap` | object\|null | metrics | Прогноз кассового разрыва; null = разрыва нет |
| `cash_gap.date` | ISO8601 | metrics | День, когда баланс уходит в минус |
| `cash_gap.projected_balance` | float64 | metrics | Баланс в этот день (отрицательный) |
| `cash_gap.shortfall` | float64 | metrics | Размер нехватки (положительное число) |
| `cash_gap.reason` | string | metrics | Текстовое объяснение причины |
| `alerts[]` | array | metrics | Структурные алерты для подсветки на UI |
| `alerts[].severity` | enum | metrics | `"info"` / `"warning"` / `"danger"` |
| `alerts[].message` | string | metrics | Текст алерта |
| `summary` | string | **llm** | 2-4 предложения финансового резюме |
| `recommendations[]` | string[] | **llm** | Практичные советы, привязанные к цифрам |

---

## Инварианты (проверяются в тестах)

- `closing_balance = opening_balance + net_cash_flow`
- `net_cash_flow = total_income − total_expense`
- `operating_cash_flow + investing_cash_flow + financing_cash_flow = net_cash_flow`
- `sum(expense_structure[].amount) = total_expense`
- `sum(expense_structure[].share) ≈ 1.0`
- Если `cash_gap != null`, то `cash_gap.projected_balance < 0` и `cash_gap.shortfall > 0`

---

## Версионирование

Поля добавляются только с обратной совместимостью (новые поля — не ломают старых клиентов).
При удалении или переименовании поля — PR с пометкой `breaking change`, согласование всей команды.
