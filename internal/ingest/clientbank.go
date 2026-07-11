package ingest

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"math"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"

	"github.com/wwpp/finaudit/internal/models"
)

func parseClientBankExchange(raw []byte) ([]models.Transaction, error) {
	text, err := decodeClientBank(raw)
	if err != nil {
		return nil, fmt.Errorf("не удалось декодировать 1CClientBankExchange: %w", err)
	}

	headAccount := ""
	var txs []models.Transaction
	inDoc := false
	doc := map[string]string{}

	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "СекцияДокумент=") {
			inDoc = true
			doc = map[string]string{}
			continue
		}
		if line == "КонецДокумента" {
			if inDoc {
				tx, ok := buildClientBankTransaction(doc, headAccount)
				if ok {
					txs = append(txs, tx)
				}
			}
			inDoc = false
			doc = map[string]string{}
			continue
		}
		if line == "КонецФайла" {
			break
		}

		key, val, ok := splitKV(line)
		if !ok {
			continue
		}
		if key == "РасчСчет" && !inDoc && headAccount == "" {
			headAccount = strings.TrimSpace(val)
			continue
		}
		if inDoc {
			doc[key] = strings.TrimSpace(val)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("ошибка чтения 1CClientBankExchange: %w", err)
	}

	if len(txs) == 0 {
		return nil, errNoTransactions
	}
	return txs, nil
}

func decodeClientBank(raw []byte) (string, error) {
	body := raw
	if len(body) >= 3 && body[0] == 0xEF && body[1] == 0xBB && body[2] == 0xBF {
		body = body[3:]
	}

	// Реальные Windows-1251 файлы не являются валидным UTF-8 (кириллица ломает
	// многобайтовые последовательности). Если байты уже валидный UTF-8 — используем как есть,
	// иначе декодируем как cp1251. Раньше здесь искали подстроку "Кодировка=Windows" в самом
	// тексте, что приводило к двойному декодированию уже-UTF-8 файлов и порче кириллицы.
	if utf8.Valid(body) {
		return string(body), nil
	}

	decoded, err := io.ReadAll(charmap.Windows1251.NewDecoder().Reader(bytes.NewReader(body)))
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

func splitKV(line string) (string, string, bool) {
	i := strings.Index(line, "=")
	if i <= 0 {
		return "", "", false
	}
	key := strings.TrimSpace(line[:i])
	val := strings.TrimSpace(line[i+1:])
	if key == "" {
		return "", "", false
	}
	return key, val, true
}

func buildClientBankTransaction(doc map[string]string, headAccount string) (models.Transaction, bool) {
	if headAccount == "" {
		return models.Transaction{}, false
	}

	date, err := parseDate(doc["Дата"])
	if err != nil {
		return models.Transaction{}, false
	}

	amount, err := parseAmount(doc["Сумма"])
	if err != nil {
		return models.Transaction{}, false
	}

	payerAcc := firstField(doc, "ПлательщикСчет", "ПлательщикРасчСчет")
	receiverAcc := firstField(doc, "ПолучательСчет", "ПолучательРасчСчет")

	tx := models.Transaction{
		Date:    date,
		Amount:  math.Abs(amount),
		Purpose: strings.TrimSpace(doc["НазначениеПлатежа"]),
	}

	switch {
	case payerAcc == headAccount:
		tx.Direction = models.Out
		tx.Counterparty = firstField(doc, "Получатель", "Получатель1", "ПолучательНаименованиеСокр", "ПолучательНаименование")
		tx.INN = firstField(doc, "ПолучательИНН", "ИННПолучателя")
	case receiverAcc == headAccount:
		tx.Direction = models.In
		tx.Counterparty = firstField(doc, "Плательщик", "Плательщик1", "ПлательщикНаименованиеСокр", "ПлательщикНаименование")
		tx.INN = firstField(doc, "ПлательщикИНН", "ИННПлательщика")
	default:
		return models.Transaction{}, false
	}

	if tx.Counterparty == "" || tx.Purpose == "" {
		return models.Transaction{}, false
	}
	if tx.Amount == 0 {
		return models.Transaction{}, false
	}
	return tx, true
}

// firstField возвращает первое непустое значение среди указанных ключей документа
// (реальные выгрузки 1С используют разные имена: Получатель / Получатель1 и т.п.).
func firstField(doc map[string]string, keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(doc[k]); v != "" {
			return v
		}
	}
	return ""
}
