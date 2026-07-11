// lex.go — загрузчик базы правовых норм (lex1). Данные встраиваются в бинарник
// через embed, чтобы compliance-модуль опирался на реальные статьи закона,
// а не на текстовые константы внутри кода.
package compliance

import (
	"embed"
	"encoding/json"
	"fmt"
)

//go:embed lexdata/legal_norms.json
var lexFS embed.FS

// LegalNorm — одна норма права, привязанная к коду проверки compliance-движка.
type LegalNorm struct {
	Code    string   `json:"code"`
	Source  string   `json:"source"`
	Article string   `json:"article"`
	Title   string   `json:"title"`
	Summary string   `json:"summary"`
	URL     string   `json:"url"`
	Tags    []string `json:"tags"`
}

type legalNormsFile struct {
	Version string      `json:"version"`
	Norms   []LegalNorm `json:"norms"`
}

var normsByCode map[string]LegalNorm

func init() {
	data, err := lexFS.ReadFile("lexdata/legal_norms.json")
	if err != nil {
		panic(fmt.Sprintf("compliance: не удалось прочитать базу норм: %v", err))
	}
	var f legalNormsFile
	if err := json.Unmarshal(data, &f); err != nil {
		panic(fmt.Sprintf("compliance: не удалось распарсить базу норм: %v", err))
	}
	normsByCode = make(map[string]LegalNorm, len(f.Norms))
	for _, n := range f.Norms {
		normsByCode[n.Code] = n
	}
}

// NormByCode возвращает норму права по коду проверки (например, "VAT_THRESHOLD").
// ok=false, если норма не найдена в базе — вызывающий код должен иметь fallback.
func NormByCode(code string) (LegalNorm, bool) {
	n, ok := normsByCode[code]
	return n, ok
}

// StatuteText — готовая строка "Источник ст. X" для поля Statute, с fallback
// на переданное значение по умолчанию, если норма не найдена в базе.
func StatuteText(code, fallback string) string {
	if n, ok := normsByCode[code]; ok {
		return fmt.Sprintf("%s %s", n.Source, n.Article)
	}
	return fallback
}

// AllNorms возвращает полный список загруженных норм (для UI/экспорта).
func AllNorms() []LegalNorm {
	out := make([]LegalNorm, 0, len(normsByCode))
	for _, n := range normsByCode {
		out = append(out, n)
	}
	return out
}
