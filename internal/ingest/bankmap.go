package ingest

import (
	"strings"
)

// detectBankFormat определяет банк-источник по заголовку CSV и возвращает
// маппинг «канонiческое имя колонки → реальное имя колонки этого банка».
func detectBankFormat(header []string) map[string]string {
	idx := buildColumnIndex(header)

	switch {
	case hasColumn(idx, "номер карты") && hasColumn(idx, "сумма операции"):
		return tinkoffMapping()
	case hasColumn(idx, "дата операции") && hasColumn(idx, "приход") && hasColumn(idx, "расход"):
		return sberMapping()
	case hasColumn(idx, "дата проводки") && hasColumn(idx, "сумма по дебету") && hasColumn(idx, "сумма по кредиту"):
		// TODO(bank2): сигнатура заголовка ориентировочная — уточнить по реальному
		// файлу Альфа-Банка, когда он появится (блокер docblk).
		return alfaMapping()
	case hasColumn(idx, "дата документа") && hasColumn(idx, "сумма платежа") && hasColumn(idx, "плательщик/получатель"):
		// TODO(bank2): сигнатура заголовка ориентировочная — уточнить по реальному
		// файлу ПСБ, когда он появится (блокер docblk).
		return psbMapping()
	default:
		return nil
	}
}

func hasColumn(idx map[string]int, name string) bool {
	_, ok := idx[name]
	return ok
}

func tinkoffMapping() map[string]string {
	return map[string]string{
		"дата операции":            "дата операции",
		"сумма дебет (руб.)":       "__tinkoff_debit__",
		"сумма кредит (руб.)":      "__tinkoff_credit__",
		"наименование контрагента": "описание",
		"инн контрагента":          "",
		"назначение платежа":       "описание",
		"категория":                "категория",
	}
}

func sberMapping() map[string]string {
	return map[string]string{
		"дата операции":            "дата операции",
		"сумма дебет (руб.)":       "расход",
		"сумма кредит (руб.)":      "приход",
		"наименование контрагента": "контрагент",
		"инн контрагента":          "инн контрагента",
		"назначение платежа":       "назначение платежа",
		"категория":                "",
	}
}

func remapHeader(header []string, mapping map[string]string) []string {
	reverse := make(map[string]string, len(mapping))
	for canonical, real := range mapping {
		if real != "" && !strings.HasPrefix(real, "__") {
			reverse[real] = canonical
		}
	}
	out := make([]string, len(header))
	for i, h := range header {
		key := strings.ToLower(strings.TrimSpace(h))
		if canonical, ok := reverse[key]; ok {
			out[i] = canonical
		} else {
			out[i] = h
		}
	}
	return out
}

// alfaMapping — маппинг колонок Альфа-Банка (CSV-выгрузка).
// TODO(bank2): проверить точные имена колонок на реальном файле — сейчас
// заполнено по типовой структуре выгрузок Альфа-Бизнес, требует сверки.
func alfaMapping() map[string]string {
	return map[string]string{
		"дата операции":            "дата проводки",
		"сумма дебет (руб.)":       "сумма по дебету",
		"сумма кредит (руб.)":      "сумма по кредиту",
		"наименование контрагента": "наименование контрагента",
		"инн контрагента":          "инн контрагента",
		"назначение платежа":       "назначение платежа",
		"категория":                "",
	}
}

// psbMapping — маппинг колонок Промсвязьбанка (CSV-выгрузка).
// TODO(bank2): проверить точные имена колонок на реальном файле — сейчас
// заполнено по типовой структуре выгрузок ПСБ Бизнес, требует сверки.
func psbMapping() map[string]string {
	return map[string]string{
		"дата операции":            "дата документа",
		"сумма дебет (руб.)":       "__psb_debit__",
		"сумма кредит (руб.)":      "__psb_credit__",
		"наименование контрагента": "плательщик/получатель",
		"инн контрагента":          "инн",
		"назначение платежа":       "назначение платежа",
		"категория":                "",
	}
}
