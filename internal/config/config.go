package config

import (
	"bufio"
	"os"
	"strings"
)

// Config — конфигурация из env. На проде заменить на viper/caarlos0-env.
type Config struct {
	HTTPAddr        string
	PostgresDSN     string
	ClickHouseAddr  string
	ClickHouseDB    string
	DeepSeekAPIKey  string
	DeepSeekModel   string
	DeepSeekBaseURL string
	AppEncKey       string // base64 (32 байта) для шифрования ключей провайдеров ИИ

	// Приём платежей (ЮKassa). Если ключи не заданы — оплата отключена,
	// остальной сервис работает без изменений.
	YooKassaShopID    string
	YooKassaSecretKey string
	PublicBaseURL     string // база для return_url после оплаты
}

func Load() (*Config, error) {
	loadDotEnv(".env") // подхватываем .env, если есть (не перетирает уже заданные env)
	return &Config{
		HTTPAddr:        env("HTTP_ADDR", ":8080"),
		PostgresDSN:     env("POSTGRES_DSN", ""),
		ClickHouseAddr:  env("CLICKHOUSE_ADDR", "localhost:9000"),
		ClickHouseDB:    env("CLICKHOUSE_DB", "finaudit"),
		DeepSeekAPIKey:  env("DEEPSEEK_API_KEY", ""),
		DeepSeekModel:   env("DEEPSEEK_MODEL", "deepseek-v4-flash"),
		DeepSeekBaseURL: env("DEEPSEEK_BASE_URL", "https://api.deepseek.com"),
		AppEncKey:       env("APP_ENC_KEY", ""),

		YooKassaShopID:    env("YOOKASSA_SHOP_ID", ""),
		YooKassaSecretKey: env("YOOKASSA_SECRET_KEY", ""),
		PublicBaseURL:     env("PUBLIC_BASE_URL", "https://finaudit.site"),
	}, nil
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// loadDotEnv читает простой .env (KEY=VALUE), не перезаписывая уже заданные переменные.
// Зависимостей нет; кривые строки и комментарии (#) пропускаются.
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return // .env нет — это норм
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.Trim(strings.TrimSpace(val), `"'`)
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, val)
		}
	}
}
