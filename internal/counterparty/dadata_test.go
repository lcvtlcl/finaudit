package counterparty

import (
	"os"
	"testing"
)

func TestNewClientFromEnv_MissingToken(t *testing.T) {
	os.Unsetenv("DADATA_API_KEY")
	_, err := NewClientFromEnv()
	if err == nil {
		t.Fatal("ожидалась ошибка при отсутствии DADATA_API_KEY")
	}
}

func TestNewClientFromEnv_WithToken(t *testing.T) {
	os.Setenv("DADATA_API_KEY", "test-token")
	defer os.Unsetenv("DADATA_API_KEY")
	c, err := NewClientFromEnv()
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if c.apiKey != "test-token" {
		t.Errorf("ожидался токен test-token, получено %q", c.apiKey)
	}
}
