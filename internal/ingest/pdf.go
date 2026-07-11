package ingest

import (
	"bufio"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ledongthuc/pdf"

	"github.com/wwpp/finaudit/internal/models"
)

// dateRe ищет дату в форматах ДД.ММ.ГГГГ или ДД.ММ.ГГ.
var dateRe = regexp.MustCompile(`\b(\d{2})\.(\d{2})\.(\d{2,4})\b`)

// signedAmountRe ищет сумму со знаком: цифры, возможно с пробелами-разделителями тысяч, запятая/точка для копеек.
var signedAmountRe = regexp.MustCompile(`[-+]?\d[\d\s]*[.,]\d{2}\b`)

// ParsePDF извлекает транзакции из текстового банковского PDF (цифровой, не сканы — OCR не используется).
func ParsePDF(path string) ([]models.Transaction, error) {
	text, err := extractPDFText(path)
	if err != nil {
		return nil, fmt.Errorf("не удалось извлечь текст из PDF: %w", err)
	}

	txs := parsePDFText(text)
	if len(txs) == 0 {
		return nil, errNoTransactions
	}
	return txs, nil
}

func extractPDFText(path string) (string, error) {
	f, r, err := pdf.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var b strings.Builder
	totalPages := r.NumPage()
	for i := 1; i <= totalPages; i++ {
		page := r.Page(i)
		if page.V.IsNull() {
			continue
		}
		content, err := page.GetPlainText(nil)
		if err != nil {
			continue
		}
		b.WriteString(content)
		b.WriteString("\n")
	}
	return b.String(), nil
}

func parsePDFText(text string) []models.Transaction {
	var txs []models.Transaction
	scanner := bufio.NewScanner(strings.NewReader(text))
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		dateMatch := dateRe.FindString(line)
		if dateMatch == "" {
			continue
		}
		date, ok := parsePDFDate(dateMatch)
		if !ok {
			continue
		}

		amountMatches := signedAmountRe.FindAllString(line, -1)
		if len(amountMatches) == 0 {
			continue
		}
		amountStr := amountMatches[len(amountMatches)-1]
		rawAmount, ok := parsePDFAmount(amountStr)
		if !ok || rawAmount == 0 {
			continue
		}

		lower := strings.ToLower(line)
		direction := models.Out
		switch {
		case strings.Contains(amountStr, "+"):
			direction = models.In
		case rawAmount < 0:
			direction = models.Out
		case strings.Contains(lower, "поступление") || strings.Contains(lower, "приход") || strings.Contains(lower, "зачисление"):
			direction = models.In
		}
		amount := math.Abs(rawAmount)

		purpose := strings.TrimSpace(strings.Replace(line, dateMatch, "", 1))
		purpose = strings.TrimSpace(strings.Replace(purpose, amountStr, "", 1))

		txs = append(txs, models.Transaction{
			Date:      date,
			Amount:    amount,
			Direction: direction,
			Purpose:   purpose,
		})
	}

	return txs
}

func parsePDFDate(s string) (time.Time, bool) {
	m := dateRe.FindStringSubmatch(s)
	if m == nil {
		return time.Time{}, false
	}
	day, _ := strconv.Atoi(m[1])
	month, _ := strconv.Atoi(m[2])
	yearStr := m[3]
	year, _ := strconv.Atoi(yearStr)
	if len(yearStr) == 2 {
		year += 2000
	}
	if day < 1 || day > 31 || month < 1 || month > 12 {
		return time.Time{}, false
	}
	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC), true
}

func parsePDFAmount(s string) (float64, bool) {
	clean := strings.ReplaceAll(s, " ", "")
	clean = strings.ReplaceAll(clean, ",", ".")
	v, err := strconv.ParseFloat(clean, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}
