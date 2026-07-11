package ingest

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"

	"github.com/xuri/excelize/v2"

	"github.com/wwpp/finaudit/internal/models"
)

var (
	errNoTransactions = errors.New("не распарсилось ни одной транзакции")
)

// deduplicate удаляет дубликаты транзакций по ключу: дата (без времени) + сумма + контрагент + назначение.
// Сохраняет первое вхождение, порядок остальных не меняет (стабильная дедупликация).
func deduplicate(txs []models.Transaction) []models.Transaction {
	seen := make(map[string]int)
	result := make([]models.Transaction, 0, len(txs))

	for _, tx := range txs {
		// Ключ уникальности: дата без времени + сумма + контрагент + назначение
		key := tx.Date.Truncate(24*time.Hour).Format("2006-01-02") + "|" +
			fmt.Sprintf("%.2f", tx.Amount) + "|" +
			tx.Counterparty + "|" +
			tx.Purpose

		if _, exists := seen[key]; !exists {
			seen[key] = 1
			result = append(result, tx)
		}
	}

	return result
}

// ParseFile — диспетчер по сигнатуре/расширению файла.
func ParseFile(path string) ([]models.Transaction, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("не удалось открыть файл: %w", err)
	}

	if hasClientBankSignature(raw) {
		txs, err := parseClientBankExchange(raw)
		if err != nil {
			return nil, err
		}
		return deduplicate(txs), nil
	}

	ext := strings.ToLower(strings.TrimPrefix(path, "."))
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '.' {
			ext = strings.ToLower(path[i+1:])
			break
		}
	}

	switch ext {
	case "csv":
		txs, err := ParseCSV(bytes.NewReader(raw))
		if err != nil {
			// фоллбэк: не банковский формат — пробуем маркетплейс (WB/Ozon)
			mpTxs, mpErr := ParseMarketplaceCSV(bytes.NewReader(raw))
			if mpErr != nil {
				return nil, err
			}
			return deduplicate(mpTxs), nil
		}
		return deduplicate(txs), nil
	case "xlsx":
		txs, err := ParseXLSX(path)
		if err != nil {
			mpTxs, mpErr := ParseMarketplaceXLSX(path)
			if mpErr != nil {
				return nil, err
			}
			return deduplicate(mpTxs), nil
		}
		return deduplicate(txs), nil
	case "pdf":
		txs, err := ParsePDF(path)
		if err != nil {
			return nil, err
		}
		return deduplicate(txs), nil
	case "txt":
		txs, err := parseSberbankTxt(raw)
		if err != nil {
			return nil, err
		}
		return deduplicate(txs), nil
	default:
		return nil, fmt.Errorf("неизвестное расширение файла: %s", ext)
	}

}

// Deduplicate убирает дубликаты транзакций (для сведения нескольких файлов пакета).
func Deduplicate(txs []models.Transaction) []models.Transaction {
	return deduplicate(txs)
}

// AccountKey извлекает расчётный счёт из выписки (для группировки файлов в пакете).
// Для 1С-обмена — РасчСчет из шапки; для CSV/XLSX счёт обычно не указан — возвращает "".
func AccountKey(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if !hasClientBankSignature(raw) {
		return ""
	}
	text, err := decodeClientBank(raw)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if strings.HasPrefix(line, "СекцияДокумент=") {
			break // дальше идут документы — шапку со счётом уже прошли
		}
		if strings.HasPrefix(line, "РасчСчет=") {
			return strings.TrimSpace(strings.TrimPrefix(line, "РасчСчет="))
		}
	}
	return ""
}

func hasClientBankSignature(raw []byte) bool {
	trimmed := raw
	if len(trimmed) >= 3 && trimmed[0] == 0xEF && trimmed[1] == 0xBB && trimmed[2] == 0xBF {
		trimmed = trimmed[3:]
	}
	lineEnd := bytes.IndexByte(trimmed, '\n')
	if lineEnd >= 0 {
		trimmed = trimmed[:lineEnd]
	}
	firstLine := strings.TrimSpace(strings.TrimSuffix(string(trimmed), "\r"))
	return firstLine == "1CClientBankExchange"
}

// ParseCSV парсит CSV-файл из io.Reader.
// Автоматически определяет формат: банковская выписка (по заголовку) или позиционный.
func ParseCSV(r io.Reader) ([]models.Transaction, error) {
	// Сначала читаем всё содержимое для детекции кодировки
	buf := &bytes.Buffer{}
	if _, err := io.Copy(buf, r); err != nil {
		return nil, fmt.Errorf("ошибка чтения CSV: %w", err)
	}
	raw := buf.Bytes()

	// Детектируем кодировку
	text, err := detectEncoding(raw)
	if err != nil {
		return nil, fmt.Errorf("ошибка детекции кодировки: %w", err)
	}

	reader := csv.NewReader(strings.NewReader(text))
	reader.Comma = ';'          // разделитель точка с запятой
	reader.FieldsPerRecord = -1 // допускаем разное число полей
	reader.LazyQuotes = true    // допускаем кавычки внутри полей

	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения CSV: %w", err)
	}

	if len(records) == 0 {
		return nil, errNoTransactions
	}

	// Сначала проверяем специфичные банковские форматы (Тинькофф/Сбер) по заголовку.
	if mapping := detectBankFormat(records[0]); mapping != nil {
		remapped := remapHeader(records[0], mapping)
		colIdx := buildColumnIndex(remapped)
		return parseBankStatementRows(records[1:], colIdx)
	}

	// Определяем формат по заголовку первой строки.
	// Банковская выписка нового формата содержит обе колонки сумм.
	colIdx := buildColumnIndex(records[0])
	_, hasDebit := colIdx["сумма дебет (руб.)"]
	_, hasCredit := colIdx["сумма кредит (руб.)"]
	if hasDebit && hasCredit {
		return parseBankStatementRows(records[1:], colIdx)
	}

	// Устаревший позиционный формат (заголовок пропускается через parseDate)
	return parseLegacyRows(records)
}

// buildColumnIndex строит маппинг «нормализованное имя колонки → индекс».
func buildColumnIndex(header []string) map[string]int {
	idx := make(map[string]int, len(header))
	for i, h := range header {
		key := strings.ToLower(strings.TrimSpace(h))
		idx[key] = i
	}
	return idx
}

// parseBankStatementRows парсит строки банковской выписки по именованным колонкам.
//
// Ожидаемые колонки:
//
//	Дата операции | Номер пп | Тип операции | Наименование контрагента |
//	ИНН контрагента | КПП контрагента | Расчетный счет контрагента |
//	БИК банка контрагента | Банк контрагента | Назначение платежа |
//	Сумма дебет (руб.) | Сумма кредит (руб.) | Валюта |
//	Остаток на конец дня (руб.) | Категория
func parseBankStatementRows(rows [][]string, col map[string]int) ([]models.Transaction, error) {
	get := func(row []string, key string) string {
		i, ok := col[key]
		if !ok || i >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[i])
	}

	var transactions []models.Transaction

	for _, row := range rows {
		dateStr := get(row, "дата операции")
		date, err := parseDate(dateStr)
		if err != nil {
			continue
		}

		directionStr := get(row, "тип операции")

		// Суммы из отдельных колонок debit/credit
		debitStr := get(row, "сумма дебет (руб.)")
		creditStr := get(row, "сумма кредит (руб.)")
		debitFilled := debitStr != ""
		creditFilled := creditStr != ""

		var amountStr string
		var direction models.Direction
		switch {
		case creditFilled && !debitFilled:
			// Только кредит заполнен — приход
			amountStr = creditStr
			direction = models.In
		case debitFilled && !creditFilled:
			// Только дебет заполнен — расход
			amountStr = debitStr
			direction = models.Out
		default:
			// Обе пустые или обе заполнены — аномалия: берём любую непустую, direction по знаку
			if debitFilled {
				amountStr = debitStr
			} else {
				amountStr = creditStr
			}
			direction = parseDirection(directionStr, 0)
		}
		var amount float64
		if amountStr == "" {
			// Обе пустые — нулевая сумма, direction по строке
			amount = 0
			direction = parseDirection(directionStr, 0)
		} else {
			var err error
			amount, err = parseAmount(amountStr)
			if err != nil {
				continue
			}
			// Если аномалия (обе заполнены/обе пустые) — уточняем direction по знаку
			if debitFilled && creditFilled {
				if amount < 0 {
					direction = models.Out
				} else {
					direction = models.In
				}
			}
		}
		counterparty := get(row, "наименование контрагента")
		inn := get(row, "инн контрагента")
		purpose := get(row, "назначение платежа")
		category := get(row, "категория")

		// Если Purpose пустое после TrimSpace - заменяем на «—»
		if strings.TrimSpace(purpose) == "" {
			purpose = "—"
		}

		transactions = append(transactions, models.Transaction{
			Date:         date,
			Amount:       math.Abs(amount),
			Direction:    direction,
			Counterparty: counterparty,
			INN:          inn,
			Purpose:      purpose,
			Category:     category,
		})
	}

	if len(transactions) == 0 {
		return nil, errNoTransactions
	}
	return transactions, nil
}

// parseLegacyRows — старый позиционный парсер (6 колонок: дата, сумма, направление, контрагент, ИНН, назначение).
func parseLegacyRows(records [][]string) ([]models.Transaction, error) {
	var transactions []models.Transaction

	for _, record := range records {
		if len(record) < 6 {
			continue
		}

		dateStr := strings.TrimSpace(record[0])
		date, err := parseDate(dateStr)
		if err != nil {
			continue
		}

		amountStr := strings.TrimSpace(record[1])
		amount, err := parseAmount(amountStr)
		if err != nil {
			continue
		}

		directionStr := strings.TrimSpace(record[2])
		direction := parseDirection(directionStr, amount)

		counterparty := strings.TrimSpace(record[3])
		inn := strings.TrimSpace(record[4])
		purpose := strings.TrimSpace(record[5])

		// Если Purpose пустое после TrimSpace - заменяем на «—»
		if purpose == "" {
			purpose = "—"
		}

		transactions = append(transactions, models.Transaction{
			Date:         date,
			Amount:       math.Abs(amount),
			Direction:    direction,
			Counterparty: counterparty,
			INN:          inn,
			Purpose:      purpose,
		})
	}

	if len(transactions) == 0 {
		return nil, errNoTransactions
	}
	return transactions, nil
}

// ParseXLSX парсит XLSX-файл (первый лист).
// Автоматически определяет формат: банковская выписка (по заголовку) или позиционный.
func ParseXLSX(path string) ([]models.Transaction, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, fmt.Errorf("не удалось открыть XLSX файл: %w", err)
	}
	defer f.Close()

	rows, err := f.GetRows(f.GetSheetName(0))
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения листа: %w", err)
	}

	if len(rows) == 0 {
		return nil, errNoTransactions
	}

	headerRow := -1
	var colIdx map[string]int
	for i, row := range rows {
		normalized := make([]string, len(row))
		for j, cell := range row {
			normalized[j] = strings.ToLower(strings.TrimSpace(cell))
		}
		mapping := detectBankFormat(normalized)
		if mapping == nil {
			continue
		}

		remapped := remapHeader(normalized, mapping)
		idx := buildColumnIndex(remapped)

		_, hasDate := idx["дата операции"]
		_, hasDebit := idx["сумма дебет (руб.)"]
		_, hasCredit := idx["сумма кредит (руб.)"]
		_, hasPurpose := idx["назначение платежа"]

		if hasDate && (hasDebit || hasCredit) && hasPurpose {
			headerRow = i
			colIdx = idx
			break
		}
	}

	if headerRow >= 0 {
		if txs, err := parseBankStatementRows(rows[headerRow+1:], colIdx); err == nil && len(txs) > 0 {
			return txs, nil
		}
		if txs, err := parseSberBusinessXLSXRows(rows[headerRow+1:], colIdx); err == nil && len(txs) > 0 {
			return txs, nil
		}
	}

	colIdx = buildColumnIndex(rows[0])
	_, hasDebit := colIdx["сумма дебет (руб.)"]
	_, hasCredit := colIdx["сумма кредит (руб.)"]
	if hasDebit && hasCredit {
		return parseBankStatementRows(rows[1:], colIdx)
	}

	// Конвертируем [][]string в records для совместимости с legacyRows
	return parseLegacyRows(rows)
}

func parseSberBusinessXLSXRows(rows [][]string, colIdx map[string]int) ([]models.Transaction, error) {
	var transactions []models.Transaction

	dateIdx, hasDate := colIdx["дата операции"]
	debitIdx, hasDebit := colIdx["сумма дебет (руб.)"]
	purposeIdx, hasPurpose := colIdx["назначение платежа"]

	if !hasDate || !hasDebit || !hasPurpose {
		return nil, errNoTransactions
	}

	for _, row := range rows {
		if len(row) == 0 {
			continue
		}

		joined := strings.ToLower(strings.TrimSpace(strings.Join(row, " ")))
		if joined == "" {
			continue
		}
		if strings.Contains(joined, "количество операций") ||
			strings.Contains(joined, "входящий остаток") ||
			strings.Contains(joined, "исходящий остаток") ||
			strings.Contains(joined, "итого оборотов") ||
			strings.Contains(joined, "всего") ||
			strings.Contains(joined, "б/с") {
			break
		}

		if dateIdx >= len(row) || debitIdx >= len(row) || purposeIdx >= len(row) {
			continue
		}

		dateRaw := strings.TrimSpace(row[dateIdx])
		debitRaw := strings.TrimSpace(row[debitIdx])
		purpose := strings.TrimSpace(row[purposeIdx])

		if dateRaw == "" || debitRaw == "" {
			continue
		}

		dateFields := strings.Fields(dateRaw)
		if len(dateFields) > 0 {
			dateRaw = dateFields[0]
		}

		date, err := parseDate(dateRaw)
		if err != nil {
			continue
		}
		amount, err := parseAmount(debitRaw)
		if err != nil || amount == 0 {
			continue
		}
		if purpose == "" {
			purpose = "—"
		}

		transactions = append(transactions, models.Transaction{
			Date:         date,
			Amount:       math.Abs(amount),
			Direction:    models.Out,
			Counterparty: "",
			INN:          "",
			Purpose:      purpose,
		})
	}

	if len(transactions) == 0 {
		return nil, errNoTransactions
	}
	return transactions, nil
}

// parseDate парсит дату в нескольких форматах.
func parseDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	formats := []string{
		"02.01.2006", // ДД.ММ.ГГГГ
		"2006-01-02", // ISO
		"02/01/2006", // ММ/ДД/ГГГГ
	}

	for _, format := range formats {
		if t, err := time.Parse(format, s); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("не удалось распарсить дату: %s", s)
}

// parseAmount парсит сумму с учётом различных разделителей тысяч и десятичного разделителя.
// Поддерживает: обычный пробел, неразрывный пробел (U+00A0), узкий неразрывный пробел (U+202F),
// апостроф и обратный апостроф как разделители тысяч.
// Десятичный разделитель: последняя запятая или точка трактуется как десятичная.
func parseAmount(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("пустая строка")
	}
	// Убираем все разделители тысяч (пробелы разных видов, апострофы)
	s = strings.ReplaceAll(s, "\u00A0", "") // неразрывный пробел
	s = strings.ReplaceAll(s, "\u202F", "") // узкий неразрывный пробел
	s = strings.ReplaceAll(s, " ", "")      // обычный пробел
	s = strings.ReplaceAll(s, "'", "")      // апостроф
	s = strings.ReplaceAll(s, "`", "")      // обратный апостроф
	// Определяем десятичный разделитель: находим последнюю запятую и последнюю точку.
	// Тот, что стоит правее, является десятичным; остальные — тысячные, их удаляем.
	lastComma := strings.LastIndex(s, ",")
	lastDot := strings.LastIndex(s, ".")
	if lastComma > lastDot {
		// Запятая — десятичный разделитель; точки (если есть) были тысячными
		s = strings.ReplaceAll(s, ".", "")
		s = strings.Replace(s, ",", ".", 1)
	} else {
		// Точка — десятичный разделитель (или нет ни того ни другого); запятые были тысячными
		s = strings.ReplaceAll(s, ",", "")
	}
	// Возвращаем ЗНАКОВОЕ число: знак нужен parseDirection как фолбэк,
	// когда колонка "Направление" пустая. Модуль берётся при сборке Transaction.
	return strconv.ParseFloat(s, 64)
}

// parseDirection определяет направление по строке или по знаку суммы.
func parseDirection(s string, amount float64) models.Direction {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "приход", "in", "вход", "поступление":
		return models.In
	case "расход", "out", "выход", "списание":
		return models.Out
	default:
		if amount < 0 {
			return models.Out
		}
		return models.In
	}
}

// detectEncoding определяет кодировку содержимого и возвращает декодированный текст.
// Поддерживает UTF-8 BOM, CP1251, CP866.
func detectEncoding(raw []byte) (string, error) {
	body := raw

	// 1. Проверяем UTF-8 BOM (ef bb bf)
	if len(body) >= 3 && body[0] == 0xEF && body[1] == 0xBB && body[2] == 0xBF {
		body = body[3:]
		return string(body), nil
	}

	// 2. Проверяем валидность как UTF-8 на первых ~4KB
	checkLen := len(body)
	if checkLen > 4096 {
		checkLen = 4096
	}
	if utf8.Valid(body[:checkLen]) {
		return string(body), nil
	}

	// 3. Пробуем Windows-1251
	decoded1251, err := io.ReadAll(charmap.Windows1251.NewDecoder().Reader(bytes.NewReader(body)))
	if err != nil {
		return "", err
	}
	decoded1251Str := string(decoded1251)

	// Проверяем, содержит ли декодированный текст валидные кириллические символы
	// и не содержит ли много replacement characters (U+FFFD)
	runeCount := 0
	fffdCount := 0
	for _, r := range decoded1251Str {
		runeCount++
		if r == utf8.RuneError {
			fffdCount++
		}
	}
	// Если replacement characters > 5% от общего количества — считаем декодирование неудачным
	if fffdCount == 0 || float64(fffdCount)/float64(runeCount) < 0.05 {
		return decoded1251Str, nil
	}

	// 4. Пробуем CodePage 866
	decoded866, err := io.ReadAll(charmap.CodePage866.NewDecoder().Reader(bytes.NewReader(body)))
	if err != nil {
		return "", err
	}
	decoded866Str := string(decoded866)

	// Проверяем replacement characters для CP866
	runeCount = 0
	fffdCount = 0
	for _, r := range decoded866Str {
		runeCount++
		if r == utf8.RuneError {
			fffdCount++
		}
	}
	if fffdCount == 0 || float64(fffdCount)/float64(runeCount) < 0.05 {
		return decoded866Str, nil
	}

	// Если всё плохо — возвращаем UTF-8 (может быть, это всё-таки UTF-8 без BOM)
	return string(body), nil
}

// newBOMStripper возвращает Reader, который пропускает UTF-8 BOM (ef bb bf) в начале потока.
// Устаревшая функция, сохранена для совместимости с clientbank.go.
// В ParseCSV теперь используется detectEncoding.
func newBOMStripper(r io.Reader) io.Reader {
	br := bufio.NewReader(r)
	// Пытаемся прочитать BOM
	b, err := br.Peek(3)
	if err == nil && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		_, _ = br.Discard(3)
	}
	return br
}
