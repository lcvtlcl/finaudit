// Package reconcile — движок сверки банковской выписки с первичными документами
// (счета, акты, УПД). Роадмап: p7.
package reconcile

import (
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/wwpp/finaudit/internal/models"
)

// Document — первичный документ для сверки (стретч p9 — УПД-XML как источник).
type Document struct {
	Number       string
	Date         string
	Amount       float64
	Counterparty string
	INN          string
}

// MatchStatus — статус сверки одной записи.
type MatchStatus string

const (
	MatchOK        MatchStatus = "matched"
	MatchNoDoc     MatchStatus = "no_document" // есть платёж, нет документа
	MatchNoPayment MatchStatus = "no_payment"  // есть документ, нет платежа
	MatchAmountGap MatchStatus = "amount_mismatch"
)

// Match — результат сверки одной пары/записи.
type Match struct {
	Transaction *models.Transaction
	Document    *Document
	Status      MatchStatus
	Diff        float64
}

// Reconcile сопоставляет транзакции и документы по сумме + ИНН/контрагенту +
// окну дат. Один документ может быть использован только один раз.
func Reconcile(txs []models.Transaction, docs []Document) []Match {
	const amountEpsilon = 0.01
	const dateWindowDays = 7

	usedDocs := make([]bool, len(docs))
	out := make([]Match, 0, len(txs)+len(docs))

	for i := range txs {
		tx := &txs[i]

		bestIdx := -1
		bestScore := -1

		for j := range docs {
			if usedDocs[j] {
				continue
			}
			doc := &docs[j]

			score := matchScore(*tx, *doc, amountEpsilon, dateWindowDays)
			if score > bestScore {
				bestScore = score
				bestIdx = j
			}
		}

		if bestIdx == -1 || bestScore < 0 {
			out = append(out, Match{
				Transaction: tx,
				Status:      MatchNoDoc,
				Diff:        tx.Amount,
			})
			continue
		}

		usedDocs[bestIdx] = true
		doc := &docs[bestIdx]
		diff := round2(tx.Amount - doc.Amount)

		status := MatchOK
		if math.Abs(diff) > amountEpsilon {
			status = MatchAmountGap
		}

		out = append(out, Match{
			Transaction: tx,
			Document:    doc,
			Status:      status,
			Diff:        diff,
		})
	}

	for j := range docs {
		if usedDocs[j] {
			continue
		}
		doc := &docs[j]
		out = append(out, Match{
			Document: doc,
			Status:   MatchNoPayment,
			Diff:     doc.Amount,
		})
	}

	return out
}

func matchScore(tx models.Transaction, doc Document, amountEpsilon float64, dateWindowDays int) int {
	score := 0

	if math.Abs(tx.Amount-doc.Amount) <= amountEpsilon {
		score += 100
	} else if math.Abs(tx.Amount-doc.Amount) <= 1.0 {
		score += 20
	}

	if sameINN(tx.INN, doc.INN) {
		score += 50
	} else if sameCounterparty(tx.Counterparty, doc.Counterparty) {
		score += 25
	}

	if withinDateWindow(tx.Date, doc.Date, dateWindowDays) {
		score += 10
	}

	if score == 0 {
		return -1
	}
	return score
}

func sameINN(a, b string) bool {
	a = digitsOnly(a)
	b = digitsOnly(b)
	return a != "" && b != "" && a == b
}

var nonAlnumRe = regexp.MustCompile(`[^\\p{L}\\p{N}]+`)

func sameCounterparty(a, b string) bool {
	a = normalizeName(a)
	b = normalizeName(b)
	return a != "" && b != "" && a == b
}

func normalizeName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "ё", "е")
	s = nonAlnumRe.ReplaceAllString(s, " ")
	s = strings.Join(strings.Fields(s), " ")
	return s
}

func digitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func withinDateWindow(txDate time.Time, docDate string, days int) bool {
	if txDate.IsZero() || strings.TrimSpace(docDate) == "" {
		return false
	}

	layouts := []string{
		"02.01.2006",
		"2006-01-02",
		time.RFC3339,
	}

	var parsed time.Time
	var err error
	for _, layout := range layouts {
		parsed, err = time.Parse(layout, docDate)
		if err == nil {
			break
		}
	}
	if err != nil {
		return false
	}

	delta := txDate.Sub(parsed)
	if delta < 0 {
		delta = -delta
	}
	return delta <= time.Duration(days)*24*time.Hour
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

func NewDocument(number, date string, amount float64, counterparty, inn string) Document {
	return Document{
		Number:       strings.TrimSpace(number),
		Date:         strings.TrimSpace(date),
		Amount:       amount,
		Counterparty: strings.TrimSpace(counterparty),
		INN:          strings.TrimSpace(inn),
	}
}
