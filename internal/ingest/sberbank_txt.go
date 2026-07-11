package ingest

import (
	"bufio"
	"regexp"
	"strings"

	"github.com/wwpp/finaudit/internal/models"
)

var sberbankFieldSplitRe = regexp.MustCompile(`\t|  +`)

func parseSberbankTxt(raw []byte) ([]models.Transaction, error) {
	scanner := bufio.NewScanner(strings.NewReader(string(raw)))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var txs []models.Transaction
	var pending *models.Transaction

	flush := func() {
		if pending != nil {
			if pending.Purpose == "" {
				pending.Purpose = pending.Category
			}
			if pending.Purpose != "" || pending.Counterparty != "" {
				txs = append(txs, *pending)
			}
		}
		pending = nil
	}

	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" ||
			trimmed == "Продолжение на следующей странице" ||
			strings.HasPrefix(trimmed, "ул. ") ||
			strings.Contains(trimmed, "www.sberbank.ru") ||
			strings.Contains(trimmed, "Сформировано в СберБанк Онлайн") ||
			strings.Contains(trimmed, "Выписка по счёту дебетовой карты") ||
			strings.Contains(trimmed, "ВЫПИСКА ПО СЧЁТУ ДЕБЕТОВОЙ КАРТЫ") ||
			strings.Contains(trimmed, "ОСТАТОК НА") ||
			strings.Contains(trimmed, "ВСЕГО СПИСАНИЙ") ||
			strings.Contains(trimmed, "MasterCard") {
			continue
		}

		fields := splitSberbankFields(trimmed)

		if len(fields) >= 5 && looksLikeDate(fields[0]) && looksLikeTime(fields[1]) &&
			looksLikeMoney(fields[len(fields)-1]) && looksLikeMoney(fields[len(fields)-2]) {

			flush()

			date, err := parseDate(fields[0])
			if err != nil {
				continue
			}
			amount, err := parseAmount(fields[len(fields)-2])
			if err != nil {
				continue
			}

			category := strings.Join(fields[2:len(fields)-2], " ")
			direction := models.Out
			lower := strings.ToLower(category)
			if strings.Contains(lower, "поступ") || strings.Contains(lower, "зачисл") ||
				strings.Contains(lower, "пополн") || strings.Contains(lower, "возврат") {
				direction = models.In
			}

			pending = &models.Transaction{
				Date:         date,
				Amount:       amount,
				Direction:    direction,
				Category:     category,
				Purpose:      "",
				Counterparty: "",
			}
			continue
		}

		if pending != nil {
			if len(fields) >= 3 && looksLikeDate(fields[0]) {
				desc := strings.Join(fields[2:], " ")
				desc = strings.TrimSpace(desc)
				if desc != "" && !looksLikeSberbankCode(desc) {
					if pending.Purpose == "" {
						pending.Purpose = desc
					} else {
						pending.Purpose += " " + desc
					}
				}
				continue
			}

			desc := strings.TrimSpace(trimmed)
			if desc != "" {
				if pending.Purpose == "" {
					pending.Purpose = desc
				} else {
					pending.Purpose += " " + desc
				}
			}
		}
	}

	flush()

	if len(txs) == 0 {
		return nil, errNoTransactions
	}
	return txs, nil
}

func splitSberbankFields(line string) []string {
	parts := sberbankFieldSplitRe.Split(line, -1)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func looksLikeDate(s string) bool {
	if len(s) != 10 {
		return false
	}
	return s[2] == '.' && s[5] == '.'
}

func looksLikeTime(s string) bool {
	if len(s) != 5 {
		return false
	}
	return s[2] == ':'
}

func looksLikeMoney(s string) bool {
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "\u00a0", "")
	return strings.Contains(s, ",") || strings.Contains(s, ".")
}

func looksLikeSberbankCode(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	// Короткие коды вроде SMP/ATM/123456 не являются назначением платежа.
	if len([]rune(s)) <= 6 && !strings.Contains(s, " ") {
		return true
	}
	return false
}
