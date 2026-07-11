package categorize

import (
	"strings"

	"github.com/wwpp/finaudit/internal/models"
)

// Categorize присваивает транзакциям Category и Activity по детерминированным правилам.
func Categorize(txs []models.Transaction) []models.Transaction {
	out := make([]models.Transaction, len(txs))
	for i, tx := range txs {
		out[i] = categorizeOne(tx)
	}
	return out
}

func categorizeOne(tx models.Transaction) models.Transaction {
	purpose := strings.ToLower(strings.TrimSpace(tx.Purpose))
	counterparty := strings.ToLower(strings.TrimSpace(tx.Counterparty))
	inn := strings.TrimSpace(tx.INN)

	// «В т.ч. НДС 20%» — пометка о включённом НДС в обычном платеже (аренда, поставка),
	// а не налоговый платёж. Отрезаем её, чтобы она не тянула операцию в «Налоги».
	for _, marker := range []string{"в т.ч. ндс", "в т.ч.ндс", "в том числе ндс", "в тч ндс", "вкл. ндс", "включая ндс"} {
		if i := strings.Index(purpose, marker); i >= 0 {
			purpose = strings.TrimSpace(purpose[:i])
			break
		}
	}

	if strings.Contains(counterparty, "входящий остаток") || strings.Contains(purpose, "остаток на начало") {
		tx.Category = "Входящий остаток"
		tx.Activity = models.ActivityOperating
		return tx
	}

	if hasAny(purpose, "оборудован", "кофемашин", "основны средств", "техник") {
		tx.Category = "Оборудование"
		tx.Activity = models.ActivityInvesting
		return tx
	}

	if hasAny(purpose, "кредит", "займ", "погашение основного долга", "дивиденд") {
		tx.Category = "Кредиты и займы"
		tx.Activity = models.ActivityFinancing
		return tx
	}

	if inn == "7701000000" || hasAny(purpose, "налог", "усн", "ндс", "ндфл", "страховы взнос", "страховые взнос") {
		tx.Category = "Налоги"
		tx.Activity = models.ActivityOperating
		return tx
	}

	if hasAny(purpose, "заработная плата", "зарплата", "аванс", "оклад") {
		tx.Category = "ФОТ"
		tx.Activity = models.ActivityOperating
		return tx
	}

	if hasAny(purpose, "аренд") {
		tx.Category = "Аренда"
		tx.Activity = models.ActivityOperating
		return tx
	}

	if hasAny(purpose, "коммунал", "электроэнерг", "связь", "интернет") {
		tx.Category = "Коммунальные услуги"
		tx.Activity = models.ActivityOperating
		return tx
	}

	if hasAny(purpose, "комисси", "эквайринг", "рко", "обслуживание счет", "ведение счет", "банковск услуг") {
		tx.Category = "Банковские комиссии"
		tx.Activity = models.ActivityOperating
		return tx
	}

	if hasAny(purpose, "реклам", "маркетинг", "директ", "таргет", "продвижен", "seo") {
		tx.Category = "Маркетинг и реклама"
		tx.Activity = models.ActivityOperating
		return tx
	}

	if hasAny(purpose, "транспорт", "логистик", "доставк", "перевозк", "гсм", "топлив", "такси") {
		tx.Category = "Транспорт и логистика"
		tx.Activity = models.ActivityOperating
		return tx
	}

	if hasAny(purpose, "подписк", "лицензи", "хостинг", "домен", "облачн", "программн обеспечен") {
		tx.Category = "ПО и подписки"
		tx.Activity = models.ActivityOperating
		return tx
	}

	if hasAny(purpose, "партия", "зерн", "молоч", "выпечк", "товар", "поставк", "закуп") {
		tx.Category = "Закупки"
		tx.Activity = models.ActivityOperating
		return tx
	}

	if hasAny(purpose, "услуг", "консультац", "юридическ", "бухгалтерск", "аутсорс", "подряд") {
		tx.Category = "Услуги и подряд"
		tx.Activity = models.ActivityOperating
		return tx
	}

	if tx.Direction == models.In {
		tx.Category = "Выручка"
		tx.Activity = models.ActivityOperating
		return tx
	}

	tx.Category = "Прочее"
	tx.Activity = models.ActivityOperating
	return tx
}

func hasAny(s string, keys ...string) bool {
	for _, key := range keys {
		if strings.Contains(s, key) {
			return true
		}
	}
	return false
}
