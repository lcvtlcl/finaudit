package main

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
	"time"

	"github.com/wwpp/finaudit/internal/models"
)

const year = 2026

var retailSeasonality = [12]float64{
	0.8, 0.75, 0.9, 0.95, 1.0, 1.05,
	1.0, 0.95, 1.05, 1.15, 1.35, 1.6,
}

// GeneratePersona детерминированно генерирует транзакции за год для персоны.
func GeneratePersona(seed int64, persona string) []models.Transaction {
	rng := rand.New(rand.NewSource(seed + personaSeedOffset(persona)))

	var txs []models.Transaction
	switch persona {
	case "ip_retail_usn6":
		txs = genRetail(rng)
	case "ooo_services_usn15":
		txs = genServices(rng)
	case "ecom_marketplace":
		txs = genEcom(rng)
	default:
		panic("unknown persona: " + persona)
	}

	sort.Slice(txs, func(i, j int) bool { return txs[i].Date.Before(txs[j].Date) })
	for i := range txs {
		txs[i].ID = int64(i + 1)
	}
	return txs
}

func personaSeedOffset(persona string) int64 {
	switch persona {
	case "ip_retail_usn6":
		return 1
	case "ooo_services_usn15":
		return 2
	case "ecom_marketplace":
		return 3
	}
	return 0
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

func addMonthlyRegular(txs *[]models.Transaction, day int, amount float64, counterparty, inn, purpose, category string, activity models.Activity) {
	for m := 1; m <= 12; m++ {
		d := time.Date(year, time.Month(m), day, 10, 0, 0, 0, time.UTC)
		*txs = append(*txs, models.Transaction{
			Date:         d,
			Amount:       amount,
			Direction:    models.Out,
			Counterparty: counterparty,
			INN:          inn,
			Purpose:      purpose,
			Category:     category,
			Activity:     activity,
		})
	}
}

// ---------- Персона 1: розница ИП на УСН 6% ----------

func genRetail(rng *rand.Rand) []models.Transaction {
	var txs []models.Transaction

	addMonthlyRegular(&txs, 5, 90000, "Арендодатель ООО «Пассаж»", "7701234567", "Аренда торговой точки", "аренда", models.ActivityOperating)
	addMonthlyRegular(&txs, 25, 65000, "Иванова А.С.", "", "Зарплата продавцу за месяц", "зарплата", models.ActivityOperating)
	addMonthlyRegular(&txs, 15, 12000, "Банк «Кредитный»", "7709998887", "Платёж по кредиту, договор №445", "кредит", models.ActivityFinancing)

	var quarterIncome [4]float64

	for m := 1; m <= 12; m++ {
		daysInMonth := time.Date(year, time.Month(m)+1, 0, 0, 0, 0, 0, time.UTC).Day()
		seasonMult := retailSeasonality[m-1]
		salesPerDay := 2 + rng.Intn(3)
		for d := 1; d <= daysInMonth; d++ {
			for s := 0; s < salesPerDay; s++ {
				base := 1500 + rng.Float64()*4500
				amount := round2(base * seasonMult)
				date := time.Date(year, time.Month(m), d, 9+rng.Intn(10), rng.Intn(60), 0, 0, time.UTC)
				txs = append(txs, models.Transaction{
					Date:         date,
					Amount:       amount,
					Direction:    models.In,
					Counterparty: "Розничный покупатель (эквайринг)",
					Purpose:      "Оплата товара, эквайринг",
					Category:     "выручка",
					Activity:     models.ActivityOperating,
				})
				quarterIncome[(m-1)/3] += amount
			}
		}
	}

	for m := 1; m <= 12; m++ {
		for _, day := range []int{10, 20} {
			amount := round2(30000 + rng.Float64()*40000)
			date := time.Date(year, time.Month(m), day, 14, 0, 0, 0, time.UTC)
			txs = append(txs, models.Transaction{
				Date:         date,
				Amount:       amount,
				Direction:    models.Out,
				Counterparty: "Поставщик товаров ООО «Опторг»",
				INN:          "5027001122",
				Purpose:      "Оплата поставки товара по счёту",
				Category:     "закупка товара",
				Activity:     models.ActivityOperating,
			})
		}
	}

	taxDates := []time.Month{4, 7, 10, 1}
	for q := 0; q < 4; q++ {
		tax := round2(quarterIncome[q] * 0.06)
		var d time.Time
		if q < 3 {
			d = time.Date(year, taxDates[q], 25, 12, 0, 0, 0, time.UTC)
		} else {
			d = time.Date(year, 12, 28, 12, 0, 0, 0, time.UTC)
		}
		txs = append(txs, models.Transaction{
			Date:         d,
			Amount:       tax,
			Direction:    models.Out,
			Counterparty: "ФНС России",
			INN:          "7727406020",
			Purpose:      fmt.Sprintf("Авансовый платёж УСН 6%% за %d квартал %d", q+1, year),
			Category:     "налоги",
			Activity:     models.ActivityOperating,
		})
	}

	return txs
}

// ---------- Персона 2: услуги ООО на УСН 15% (с кассовым разрывом в Q3) ----------

func genServices(rng *rand.Rand) []models.Transaction {
	var txs []models.Transaction

	addMonthlyRegular(&txs, 5, 180000, "Арендодатель ЗАО «Бизнес-центр»", "7712345678", "Аренда офиса", "аренда", models.ActivityOperating)
	addMonthlyRegular(&txs, 25, 420000, "ФОТ сотрудников (реестр)", "", "Заработная плата сотрудникам за месяц", "зарплата", models.ActivityOperating)
	addMonthlyRegular(&txs, 12, 55000, "Банк «Кредитный»", "7709998887", "Платёж по кредиту, договор №778", "кредит", models.ActivityFinancing)
	addMonthlyRegular(&txs, 1, 8000, "Сервис Такое SaaS", "9909112233", "Подписка на CRM-систему", "подписки", models.ActivityOperating)

	var quarterIncome, quarterExpense [4]float64

	for m := 1; m <= 12; m++ {
		count := 3 + rng.Intn(3)
		for i := 0; i < count; i++ {
			amount := round2(80000 + rng.Float64()*220000)
			day := 1 + rng.Intn(27)
			date := time.Date(year, time.Month(m), day, 11, rng.Intn(60), 0, 0, time.UTC)
			txs = append(txs, models.Transaction{
				Date:         date,
				Amount:       amount,
				Direction:    models.In,
				Counterparty: fmt.Sprintf("Клиент ООО «Заказчик-%d»", 1+rng.Intn(9)),
				INN:          fmt.Sprintf("77%08d", rng.Intn(100000000)),
				Purpose:      "Оплата по договору оказания услуг",
				Category:     "выручка",
				Activity:     models.ActivityOperating,
			})
			q := (m - 1) / 3
			quarterIncome[q] += amount
		}
	}

	// КАССОВЫЙ РАЗРЫВ в Q3: крупная предоплата 1 июля, оплата от клиента только 20 июля.
	prepayDate := time.Date(year, 7, 1, 10, 0, 0, 0, time.UTC)
	prepayAmount := 950000.0
	txs = append(txs, models.Transaction{
		Date:         prepayDate,
		Amount:       prepayAmount,
		Direction:    models.Out,
		Counterparty: "Поставщик ООО «Субподрядчик Плюс»",
		INN:          "5029887766",
		Purpose:      "Предоплата 100% за услуги субподряда по договору №112",
		Category:     "закупка услуг",
		Activity:     models.ActivityOperating,
	})
	quarterExpense[2] += prepayAmount

	clientPayDate := time.Date(year, 7, 20, 15, 0, 0, 0, time.UTC)
	clientPayAmount := 1100000.0
	txs = append(txs, models.Transaction{
		Date:         clientPayDate,
		Amount:       clientPayAmount,
		Direction:    models.In,
		Counterparty: "Клиент ООО «Крупный заказчик»",
		INN:          "7788990011",
		Purpose:      "Оплата по договору №501 за проект",
		Category:     "выручка",
		Activity:     models.ActivityOperating,
	})
	quarterIncome[2] += clientPayAmount

	for q := 0; q < 4; q++ {
		base := quarterIncome[q] - quarterExpense[q]
		if base < 0 {
			base = 0
		}
		tax := round2(base * 0.15)
		var d time.Time
		if q < 3 {
			d = time.Date(year, time.Month(4+q*3), 25, 12, 0, 0, 0, time.UTC)
		} else {
			d = time.Date(year, 12, 28, 12, 0, 0, 0, time.UTC)
		}
		txs = append(txs, models.Transaction{
			Date:         d,
			Amount:       tax,
			Direction:    models.Out,
			Counterparty: "ФНС России",
			INN:          "7727406020",
			Purpose:      fmt.Sprintf("Авансовый платёж УСН 15%% за %d квартал %d", q+1, year),
			Category:     "налоги",
			Activity:     models.ActivityOperating,
		})
	}

	return txs
}

// ---------- Персона 3: e-commerce на маркетплейсах ----------

func genEcom(rng *rand.Rand) []models.Transaction {
	var txs []models.Transaction

	addMonthlyRegular(&txs, 5, 40000, "Арендодатель склада", "7755001122", "Аренда склада", "аренда", models.ActivityOperating)
	addMonthlyRegular(&txs, 25, 150000, "ФОТ сотрудников (реестр)", "", "Зарплата сотрудникам за месяц", "зарплата", models.ActivityOperating)
	addMonthlyRegular(&txs, 18, 20000, "Банк «Кредитный»", "7709998887", "Платёж по кредиту, договор №221", "кредит", models.ActivityFinancing)

	var quarterIncome [4]float64

	for m := 1; m <= 12; m++ {
		seasonMult := retailSeasonality[m-1]
		for _, day := range []int{10, 25} {
			gross := round2((250000 + rng.Float64()*350000) * seasonMult)
			commission := round2(gross * 0.18)
			payout := round2(gross - commission)
			date := time.Date(year, time.Month(m), day, 12, 0, 0, 0, time.UTC)
			platform := "Wildberries"
			inn := "7721617461"
			if day == 25 {
				platform = "Ozon"
				inn = "7735642672"
			}
			txs = append(txs, models.Transaction{
				Date:         date,
				Amount:       payout,
				Direction:    models.In,
				Counterparty: platform,
				INN:          inn,
				Purpose:      fmt.Sprintf("Выплата за реализованный товар (%s), за минусом комиссии", platform),
				Category:     "выручка маркетплейс",
				Activity:     models.ActivityOperating,
			})
			q := (m - 1) / 3
			quarterIncome[q] += payout
		}
	}

	for m := 1; m <= 12; m++ {
		amount := round2(120000 + rng.Float64()*180000)
		date := time.Date(year, time.Month(m), 8, 10, 0, 0, 0, time.UTC)
		txs = append(txs, models.Transaction{
			Date:         date,
			Amount:       amount,
			Direction:    models.Out,
			Counterparty: "Фабрика-производитель «Восток Пром»",
			INN:          "2701998877",
			Purpose:      "Оплата производства партии товара",
			Category:     "закупка товара",
			Activity:     models.ActivityOperating,
		})
	}

	for q := 0; q < 4; q++ {
		tax := round2(quarterIncome[q] * 0.06)
		var d time.Time
		if q < 3 {
			d = time.Date(year, time.Month(4+q*3), 25, 12, 0, 0, 0, time.UTC)
		} else {
			d = time.Date(year, 12, 28, 12, 0, 0, 0, time.UTC)
		}
		txs = append(txs, models.Transaction{
			Date:         d,
			Amount:       tax,
			Direction:    models.Out,
			Counterparty: "ФНС России",
			INN:          "7727406020",
			Purpose:      fmt.Sprintf("Авансовый платёж УСН 6%% за %d квартал %d", q+1, year),
			Category:     "налоги",
			Activity:     models.ActivityOperating,
		})
	}

	return txs
}

func quarterOf(t time.Time) int {
	return (int(t.Month())-1)/3 + 1
}
