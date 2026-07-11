package compliance

import "testing"

func TestLegalNormsLoaded(t *testing.T) {
	required := []string{
		"VAT_THRESHOLD", "USN_LIMIT", "CASH_115FZ", "PERSONAL_54_1",
		"UK_198", "UK_199", "UK_172_1", "UK_174", "FZ_402_ACCOUNTING",
	}
	for _, code := range required {
		n, ok := NormByCode(code)
		if !ok {
			t.Errorf("норма %s не найдена в базе", code)
			continue
		}
		if n.Source == "" || n.Article == "" || n.Title == "" || n.Summary == "" || n.URL == "" {
			t.Errorf("норма %s: пустое обязательное поле: %+v", code, n)
		}
	}
	if len(AllNorms()) < len(required) {
		t.Errorf("ожидалось минимум %d норм, загружено %d", len(required), len(AllNorms()))
	}
}

func TestStatuteTextFallback(t *testing.T) {
	if got := StatuteText("VAT_THRESHOLD", "fallback"); got == "fallback" {
		t.Error("ожидалась норма из базы, получен fallback")
	}
	if got := StatuteText("NONEXISTENT_CODE", "fallback"); got != "fallback" {
		t.Errorf("ожидался fallback для несуществующего кода, получено %q", got)
	}
}
