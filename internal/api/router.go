package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/wwpp/finaudit/internal/categorize"
	"github.com/wwpp/finaudit/internal/checks"
	"github.com/wwpp/finaudit/internal/compliance"
	"github.com/wwpp/finaudit/internal/config"
	"github.com/wwpp/finaudit/internal/counterparty"
	"github.com/wwpp/finaudit/internal/crypto"
	"github.com/wwpp/finaudit/internal/export"
	"github.com/wwpp/finaudit/internal/forecast"
	"github.com/wwpp/finaudit/internal/ingest"
	"github.com/wwpp/finaudit/internal/llm"
	"github.com/wwpp/finaudit/internal/metrics"
	"github.com/wwpp/finaudit/internal/models"
	"github.com/wwpp/finaudit/internal/rating"
	"github.com/wwpp/finaudit/internal/simulate"
	"github.com/wwpp/finaudit/internal/storage/postgres"
)

type contextKey string

const (
	sessionCookieName            = "session"
	userIDContextKey  contextKey = "user_id"
)

// UserIDFromContext возвращает userID, если он был проставлен auth middleware.
func UserIDFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(userIDContextKey).(string)
	return v, ok
}

// sessionTTL — срок жизни сессии; продлевается при активности (скользящее окно).
const sessionTTL = 30 * 24 * time.Hour

// sessionStore хранит сессии в Postgres (переживают перезапуск/редеплой).
// Если БД недоступна — fallback на in-memory (локальный запуск без Postgres).
type sessionStore struct {
	mu     sync.RWMutex
	tokens map[string]int64
	db     *postgres.Store
}

func newSessionStore(db *postgres.Store) *sessionStore {
	return &sessionStore{tokens: make(map[string]int64), db: db}
}

func (s *sessionStore) create(userID int64) (string, error) {
	token, err := randomToken(32)
	if err != nil {
		return "", err
	}
	if s.db != nil {
		// Успех → сессия персистентна. Ошибка (нет таблицы/БД недоступна) → не валим
		// вход, деградируем в in-memory (сессия проживёт до перезапуска).
		if err := s.db.CreateSession(context.Background(), token, userID, time.Now().Add(sessionTTL)); err == nil {
			return token, nil
		}
	}
	s.mu.Lock()
	s.tokens[token] = userID
	s.mu.Unlock()
	return token, nil
}

func (s *sessionStore) get(token string) (int64, bool) {
	if s.db != nil {
		if id, ok, err := s.db.GetSession(context.Background(), token); err == nil && ok {
			return id, true
		}
	}
	s.mu.RLock()
	id, ok := s.tokens[token]
	s.mu.RUnlock()
	return id, ok
}

// touch продлевает срок жизни сессии (скользящее окно) — только для персистентного хранилища.
func (s *sessionStore) touch(token string) {
	if s.db != nil {
		_ = s.db.TouchSession(context.Background(), token, time.Now().Add(sessionTTL))
	}
}

func (s *sessionStore) delete(token string) {
	if s.db != nil {
		_ = s.db.DeleteSession(context.Background(), token)
	}
	s.mu.Lock()
	delete(s.tokens, token)
	s.mu.Unlock()
}

func randomToken(size int) (string, error) {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("token generate: %w", err)
	}
	return hex.EncodeToString(b), nil
}

type authAPI struct {
	store    *postgres.Store
	sessions *sessionStore
}

func newAuthAPI(store *postgres.Store) *authAPI {
	return &authAPI{store: store, sessions: newSessionStore(store)}
}

func (a *authAPI) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookieName)
		if err != nil || c.Value == "" {
			next.ServeHTTP(w, r)
			return
		}
		if userID, ok := a.sessions.get(c.Value); ok {
			// Скользящее продление: активность продлевает сессию и обновляет cookie.
			a.sessions.touch(c.Value)
			setSessionCookie(w, c.Value)
			ctx := context.WithValue(r.Context(), userIDContextKey, strconv.FormatInt(userID, 10))
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *authAPI) registerRoutes(r chi.Router) {
	r.Post("/api/register", a.handleRegister())
	r.Post("/api/login", a.handleLogin())
	r.Post("/api/logout", a.handleLogout())
	r.Get("/api/me", a.handleMe())
}

type settingsResponse struct {
	Model      string   `json:"model"`
	KeyMasked  string   `json:"key_masked"`
	TokensUsed int64    `json:"tokens_used"`
	Models     []string `json:"models"`
}

type updateModelRequest struct {
	Model string `json:"model"`
}

type updateKeyRequest struct {
	Key string `json:"key"`
}

func registerSettingsRoutes(r chi.Router) {
	r.Get("/api/settings", handleGetSettings())
	r.Post("/api/settings", handleUpdateSettings())
	r.Post("/api/settings/key", handleUpdateSettingsKey())
}

func handleGetSettings() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(settingsResponse{
			Model:      llm.CurrentModel(),
			KeyMasked:  llm.CurrentKeyMasked(),
			TokensUsed: llm.TokensUsed(),
			Models:     llm.AvailableModels(),
		})
	}
}

func handleUpdateSettings() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req updateModelRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpError(w, http.StatusBadRequest, "некорректный JSON")
			return
		}
		if err := llm.SetModel(req.Model); err != nil {
			httpError(w, http.StatusBadRequest, "неподдерживаемая модель")
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(settingsResponse{
			Model:      llm.CurrentModel(),
			KeyMasked:  llm.CurrentKeyMasked(),
			TokensUsed: llm.TokensUsed(),
			Models:     llm.AvailableModels(),
		})
	}
}

func handleUpdateSettingsKey() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req updateKeyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpError(w, http.StatusBadRequest, "некорректный JSON")
			return
		}
		if strings.TrimSpace(req.Key) == "" {
			httpError(w, http.StatusBadRequest, "ключ обязателен")
			return
		}
		llm.SetAPIKey(req.Key)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]string{"key_masked": llm.CurrentKeyMasked()})
	}
}

// NewRouter — точка сборки HTTP-роутера.
// Метаданные сборки. Подставляются при билде через -ldflags:
//
//	-X github.com/wwpp/finaudit/internal/api.BuildCommit=<sha>
//	-X github.com/wwpp/finaudit/internal/api.BuildTime=<iso8601>
//
// Если не заданы — значения по умолчанию (локальная сборка).
var (
	BuildCommit = "dev"
	BuildTime   = "unknown"
)

func NewRouter(logger *slog.Logger, cfg *config.Config, store *postgres.Store) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)

	auth := newAuthAPI(store)
	r.Use(auth.middleware)
	auth.registerRoutes(r)
	llm.InitRuntimeFromConfig(cfg)
	registerSettingsRoutes(r)

	cipher, err := crypto.New(cfg.AppEncKey)
	if err != nil {
		logger.Warn("персональные ключи ИИ выключены (APP_ENC_KEY не задан/неверен)", "err", err)
		cipher = nil
	}
	newCredAPI(store, cipher).registerRoutes(r)
	newBatchAPI(store, cfg, logger, cipher).registerRoutes(r)
	newDocAPI(store, cfg, logger, cipher).registerRoutes(r)
	newPlannedAPI(store).registerRoutes(r)

	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// Какая версия кода реально задеплоена (commit + время сборки).
	r.Get("/version", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"commit": BuildCommit,
			"built":  BuildTime,
		})
	})

	// Загрузка выписки -> весь конвейер -> AuditResult JSON.
	r.Post("/audit", handleAudit(cfg, logger, store, cipher))

	// Та же выписка -> готовый Excel-отчёт (скачивание).
	r.Post("/export", handleExport(store))

	// Просмотр распарсенных транзакций.
	r.Post("/transactions", handleTransactions())

	// Статика дашборда из ./web (index.html отдаётся на /).
	r.Handle("/*", http.FileServer(http.Dir("web")))

	// История загрузок пользователя.
	r.Get("/api/uploads", handleListUploads(store))
	r.Get("/api/uploads/{id}", handleGetUpload(store))
	r.Get("/api/uploads/{id}/export", handleExportUpload(store))
	r.Delete("/api/uploads/{id}", handleDeleteUpload(store))
	r.Post("/api/uploads/{id}/simulate", handleSimulate(store))
	r.Get("/api/uploads/{id}/transactions", handleUploadTransactions(store))
	r.Post("/api/uploads/{id}/edit", handleEditTransactions(store))

	return r
}

type registerRequest struct {
	Email     string `json:"email"`
	Password  string `json:"password"`
	Name      string `json:"name"`
	Company   string `json:"company"`
	LegalForm string `json:"legal_form"`
	TaxRegime string `json:"tax_regime"`
}

// нормализуем значения профиля налогоплательщика (дефолт ip/usn при пустом/неизвестном).
func normLegalForm(v string) string {
	switch v {
	case "ip", "ooo", "self_employed":
		return v
	default:
		return "ip"
	}
}

func normTaxRegime(v string) string {
	switch v {
	case "usn", "npd", "osno":
		return v
	default:
		return "usn"
	}
}

// taxProfileForUser грузит налоговый профиль пользователя по строковому userID (из auth-контекста).
// При любой ошибке возвращает дефолт (ip/usn), чтобы анализ не падал. Используется в точках
// вызова checks.Run/compliance.Run для ветвления по режиму.
func taxProfileForUser(ctx context.Context, store *postgres.Store, userIDStr string) models.TaxProfile {
	def := models.TaxProfile{TaxRegime: "usn", LegalForm: "ip"}
	if store == nil {
		return def
	}
	id, err := strconv.ParseInt(userIDStr, 10, 64)
	if err != nil {
		return def
	}
	u, err := store.GetUserByID(ctx, id)
	if err != nil {
		return def
	}
	return models.TaxProfile{TaxRegime: normTaxRegime(u.TaxRegime), LegalForm: normLegalForm(u.LegalForm)}
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type meResponse struct {
	ID        int64  `json:"id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	Company   string `json:"company"`
	LegalForm string `json:"legal_form"`
	TaxRegime string `json:"tax_regime"`
}

func (a *authAPI) requireStore(w http.ResponseWriter) bool {
	if a.store != nil {
		return true
	}
	httpError(w, http.StatusServiceUnavailable, "авторизация недоступна: нет БД")
	return false
}

func (a *authAPI) handleRegister() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.requireStore(w) {
			return
		}
		var req registerRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpError(w, http.StatusBadRequest, "некорректный JSON")
			return
		}
		req.Email = strings.TrimSpace(req.Email)
		req.Name = strings.TrimSpace(req.Name)
		req.Company = strings.TrimSpace(req.Company)

		if req.Email == "" {
			httpError(w, http.StatusBadRequest, "email обязателен")
			return
		}
		if len(req.Password) < 8 {
			httpError(w, http.StatusBadRequest, "пароль должен быть не короче 8 символов")
			return
		}

		hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "ошибка хэширования пароля")
			return
		}

		u, err := a.store.CreateUser(r.Context(), req.Email, string(hash), req.Name, req.Company, normLegalForm(req.LegalForm), normTaxRegime(req.TaxRegime))
		if err != nil {
			if errors.Is(err, postgres.ErrEmailTaken) {
				httpError(w, http.StatusConflict, "email занят")
				return
			}
			httpError(w, http.StatusInternalServerError, "ошибка регистрации")
			return
		}

		token, err := a.sessions.create(u.ID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "ошибка создания сессии")
			return
		}
		setSessionCookie(w, token)

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(meResponse{ID: u.ID, Email: u.Email, Name: u.Name, Company: u.Company, LegalForm: u.LegalForm, TaxRegime: u.TaxRegime})
	}
}

func (a *authAPI) handleLogin() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.requireStore(w) {
			return
		}
		var req loginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpError(w, http.StatusBadRequest, "некорректный JSON")
			return
		}
		req.Email = strings.TrimSpace(req.Email)

		if req.Email == "" {
			httpError(w, http.StatusBadRequest, "email обязателен")
			return
		}
		if len(req.Password) < 8 {
			httpError(w, http.StatusBadRequest, "пароль должен быть не короче 8 символов")
			return
		}

		u, err := a.store.GetUserByEmail(r.Context(), req.Email)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				httpError(w, http.StatusUnauthorized, "неверный email или пароль")
				return
			}
			httpError(w, http.StatusInternalServerError, "ошибка входа")
			return
		}

		if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password)); err != nil {
			httpError(w, http.StatusUnauthorized, "неверный email или пароль")
			return
		}

		token, err := a.sessions.create(u.ID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "ошибка создания сессии")
			return
		}
		setSessionCookie(w, token)

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(meResponse{ID: u.ID, Email: u.Email, Name: u.Name, Company: u.Company, LegalForm: u.LegalForm, TaxRegime: u.TaxRegime})
	}
}

func (a *authAPI) handleLogout() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(sessionCookieName); err == nil && c.Value != "" {
			a.sessions.delete(c.Value)
		}
		clearSessionCookie(w)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}
}

func (a *authAPI) handleMe() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.requireStore(w) {
			return
		}
		uidStr, ok := UserIDFromContext(r.Context())
		if !ok {
			httpError(w, http.StatusUnauthorized, "не авторизован")
			return
		}
		uid, err := strconv.ParseInt(uidStr, 10, 64)
		if err != nil {
			httpError(w, http.StatusUnauthorized, "не авторизован")
			return
		}
		u, err := a.store.GetUserByID(r.Context(), uid)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				httpError(w, http.StatusUnauthorized, "не авторизован")
				return
			}
			httpError(w, http.StatusInternalServerError, "ошибка чтения профиля")
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(meResponse{ID: u.ID, Email: u.Email, Name: u.Name, Company: u.Company, LegalForm: u.LegalForm, TaxRegime: u.TaxRegime})
	}
}

func setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionTTL / time.Second),
	})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
	})
}

// maxUploadBytes — лимит размера одиночной загрузки; maxBatchBytes — для мультифайловых (пакет/база).
const (
	maxUploadBytes = 15 << 20 // 15 МБ
	maxBatchBytes  = 60 << 20 // 60 МБ на пачку
)

// allowedUpload проверяет расширение файла (защита от произвольных типов).
func allowedUpload(filename string) bool {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".csv", ".xlsx", ".txt", ".pdf":
		return true
	default:
		return false
	}
}

func handleAudit(cfg *config.Config, logger *slog.Logger, store *postgres.Store, cipher *crypto.Cipher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			httpError(w, http.StatusBadRequest, "файл не прочитан или слишком большой (лимит 15 МБ)")
			return
		}

		file, header, err := r.FormFile("file")
		if err != nil {
			httpError(w, http.StatusBadRequest, "файл не передан (ожидается поле 'file')")
			return
		}
		if !allowedUpload(header.Filename) {
			httpError(w, http.StatusBadRequest, "неподдерживаемый тип файла (нужен CSV, XLSX, TXT/1С или PDF)")
			return
		}

		storedPath, checksum, size, err := saveUploadedFile(file, header.Filename)
		if err != nil {
			http.Error(w, "failed to save uploaded file", http.StatusInternalServerError)
			return
		}

		_ = checksum
		_ = size

		txs, err := ingest.ParseFile(storedPath)
		if err != nil {
			httpError(w, http.StatusUnprocessableEntity, "не удалось распарсить выписку: "+err.Error())
			return
		}

		txs = categorize.Categorize(txs)

		userID, _ := UserIDFromContext(r.Context())
		var uploadID int64
		if store != nil {
			uploadID, err = store.CreateUpload(r.Context(), userID, header.Filename)
			if err != nil {
				logger.Warn("store: create upload", "err", err)
				uploadID = 0
			}
		}
		if store != nil && uploadID > 0 {
			if err := store.BulkInsertTransactions(r.Context(), uploadID, txs); err != nil {
				logger.Warn("store: bulk insert", "err", err)
			}
		}

		profile := taxProfileForUser(r.Context(), store, userID)
		res := metrics.ComputeAudit(txs)
		res.TaxRegime = profile.TaxRegime
		res.Checks = checks.Run(res, txs, profile)
		var planned []models.PlannedPayment
		if store != nil {
			planned, _ = store.ListPlanned(r.Context(), userID)
		}
		res.Forecast = forecast.Build(txs, res.ClosingBalance, res.Period.To, 60, planned)
		cpClient, _ := counterparty.NewClientFromEnv()
		res.Compliance = compliance.Run(res, txs, counterparty.CollectStatuses(r.Context(), cpClient, txs), profile)
		res.Scenarios = simulate.Scenarios(res, txs)
		res.Rating = rating.Compute(res)

		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel()

		client := llm.New(cfg)
		enriched := false
		if userID != "" && cipher != nil && store != nil {
			if uid, e := strconv.ParseInt(userID, 10, 64); e == nil {
				if provider, model, keyEnc, e2 := store.ActiveCredentialSecret(ctx, uid); e2 == nil {
					if key, e3 := cipher.Decrypt(keyEnc); e3 == nil {
						base := ""
						if p, ok := llm.GetProvider(provider); ok {
							base = p.BaseURL
						}
						if tokens, e4 := client.EnrichWith(ctx, &res, base, string(key), model); e4 != nil {
							logger.Warn("llm enrich (user key) failed", "err", e4)
						} else {
							_ = store.AddTokens(ctx, uid, tokens)
							enriched = true
						}
					}
				}
			}
		}
		if !enriched {
			if err := client.Enrich(ctx, &res); err != nil {
				logger.Warn("llm enrich failed", "err", err)
			}
		}

		if store != nil && uploadID > 0 {
			res.UploadID = uploadID
			if err := store.SaveAuditResult(r.Context(), uploadID, res); err != nil {
				logger.Warn("store: save audit result", "err", err)
			}
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(res)
	}
}

func handleTransactions() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			httpError(w, http.StatusBadRequest, "файл не прочитан или слишком большой (лимит 15 МБ)")
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			httpError(w, http.StatusBadRequest, "файл не передан (ожидается поле 'file')")
			return
		}
		if !allowedUpload(header.Filename) {
			httpError(w, http.StatusBadRequest, "неподдерживаемый тип файла (нужен CSV, XLSX, TXT/1С или PDF)")
			return
		}
		defer file.Close()

		ext := filepath.Ext(header.Filename)
		if ext == "" {
			ext = ".csv"
		}
		tmp, err := os.CreateTemp("", "finaudit-*"+ext)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "временный файл")
			return
		}
		defer os.Remove(tmp.Name())
		if _, err := io.Copy(tmp, file); err != nil {
			tmp.Close()
			httpError(w, http.StatusInternalServerError, "запись файла")
			return
		}
		tmp.Close()

		txs, err := ingest.ParseFile(tmp.Name())
		if err != nil {
			httpError(w, http.StatusUnprocessableEntity, "не удалось распарсить выписку: "+err.Error())
			return
		}
		txs = categorize.Categorize(txs)

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(struct {
			Count        int                  `json:"count"`
			Transactions []models.Transaction `json:"transactions"`
		}{
			Count:        len(txs),
			Transactions: txs,
		})
	}
}

func handleExport(store *postgres.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			httpError(w, http.StatusBadRequest, "файл не прочитан или слишком большой (лимит 15 МБ)")
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			httpError(w, http.StatusBadRequest, "файл не передан (ожидается поле 'file')")
			return
		}
		if !allowedUpload(header.Filename) {
			httpError(w, http.StatusBadRequest, "неподдерживаемый тип файла (нужен CSV, XLSX, TXT/1С или PDF)")
			return
		}
		defer file.Close()

		ext := filepath.Ext(header.Filename)
		if ext == "" {
			ext = ".csv"
		}
		tmp, err := os.CreateTemp("", "finaudit-*"+ext)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "временный файл")
			return
		}
		defer os.Remove(tmp.Name())
		if _, err := io.Copy(tmp, file); err != nil {
			tmp.Close()
			httpError(w, http.StatusInternalServerError, "запись файла")
			return
		}
		tmp.Close()

		txs, err := ingest.ParseFile(tmp.Name())
		if err != nil {
			httpError(w, http.StatusUnprocessableEntity, "не удалось распарсить выписку: "+err.Error())
			return
		}
		txs = categorize.Categorize(txs)
		exportUserID, _ := UserIDFromContext(r.Context())
		profile := taxProfileForUser(r.Context(), store, exportUserID)
		res := metrics.ComputeAudit(txs)
		res.TaxRegime = profile.TaxRegime
		res.Checks = checks.Run(res, txs, profile)

		data, err := export.ToExcel(res)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "сборка отчёта: "+err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
		w.Header().Set("Content-Disposition", `attachment; filename="finaudit.xlsx"`)
		_, _ = w.Write(data)
	}
}

func httpError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func handleListUploads(store *postgres.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if store == nil {
			httpError(w, http.StatusServiceUnavailable, "авторизация недоступна: нет БД")
			return
		}
		userID, ok := UserIDFromContext(r.Context())
		if !ok || userID == "" {
			httpError(w, http.StatusUnauthorized, "не авторизован")
			return
		}

		uploads, err := store.ListUploadsWithSummary(r.Context(), userID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "не удалось получить историю загрузок")
			return
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(uploads)
	}
}

func handleGetUpload(store *postgres.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if store == nil {
			httpError(w, http.StatusServiceUnavailable, "авторизация недоступна: нет БД")
			return
		}
		userID, ok := UserIDFromContext(r.Context())
		if !ok || userID == "" {
			httpError(w, http.StatusUnauthorized, "не авторизован")
			return
		}
		uploadID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil || uploadID <= 0 {
			httpError(w, http.StatusBadRequest, "некорректный id загрузки")
			return
		}

		items, err := store.ListUploadsWithSummary(r.Context(), userID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "не удалось проверить доступ к загрузке")
			return
		}
		allowed := false
		for _, item := range items {
			if item.ID == uploadID {
				allowed = true
				break
			}
		}
		if !allowed {
			httpError(w, http.StatusNotFound, "загрузка не найдена")
			return
		}

		result, err := store.GetAuditResult(r.Context(), uploadID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				httpError(w, http.StatusNotFound, "результат не найден")
				return
			}
			httpError(w, http.StatusInternalServerError, "не удалось получить результат")
			return
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(result)
	}
}

func userOwnsUpload(store *postgres.Store, ctx context.Context, userID string, uploadID int64) (bool, error) {
	items, err := store.ListUploadsWithSummary(ctx, userID)
	if err != nil {
		return false, err
	}
	for _, it := range items {
		if it.ID == uploadID {
			return true, nil
		}
	}
	return false, nil
}

func handleExportUpload(store *postgres.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if store == nil {
			httpError(w, http.StatusServiceUnavailable, "нет БД")
			return
		}
		userID, ok := UserIDFromContext(r.Context())
		if !ok || userID == "" {
			httpError(w, http.StatusUnauthorized, "не авторизован")
			return
		}
		uploadID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil || uploadID <= 0 {
			httpError(w, http.StatusBadRequest, "некорректный id загрузки")
			return
		}
		owns, err := userOwnsUpload(store, r.Context(), userID, uploadID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "не удалось проверить доступ")
			return
		}
		if !owns {
			httpError(w, http.StatusNotFound, "загрузка не найдена")
			return
		}
		result, err := store.GetAuditResult(r.Context(), uploadID)
		if err != nil {
			httpError(w, http.StatusNotFound, "результат не найден")
			return
		}
		data, err := export.ToExcel(result)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "не удалось построить Excel")
			return
		}
		w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=finaudit-%d.xlsx", uploadID))
		_, _ = w.Write(data)
	}
}

func handleDeleteUpload(store *postgres.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if store == nil {
			httpError(w, http.StatusServiceUnavailable, "нет БД")
			return
		}
		userID, ok := UserIDFromContext(r.Context())
		if !ok || userID == "" {
			httpError(w, http.StatusUnauthorized, "не авторизован")
			return
		}
		uploadID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil || uploadID <= 0 {
			httpError(w, http.StatusBadRequest, "некорректный id загрузки")
			return
		}
		if err := store.DeleteUpload(r.Context(), userID, uploadID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				httpError(w, http.StatusNotFound, "загрузка не найдена")
				return
			}
			httpError(w, http.StatusInternalServerError, "не удалось удалить загрузку")
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	}
}

// handleUploadTransactions отдаёт сохранённые транзакции загрузки (для ручной правки).
func handleUploadTransactions(store *postgres.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if store == nil {
			httpError(w, http.StatusServiceUnavailable, "нет БД")
			return
		}
		userID, ok := UserIDFromContext(r.Context())
		if !ok || userID == "" {
			httpError(w, http.StatusUnauthorized, "не авторизован")
			return
		}
		uploadID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil || uploadID <= 0 {
			httpError(w, http.StatusBadRequest, "некорректный id загрузки")
			return
		}
		owns, err := userOwnsUpload(store, r.Context(), userID, uploadID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "не удалось проверить доступ")
			return
		}
		if !owns {
			httpError(w, http.StatusNotFound, "загрузка не найдена")
			return
		}
		txs, err := store.GetTransactionsByUpload(r.Context(), uploadID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "не удалось прочитать транзакции")
			return
		}
		if txs == nil {
			txs = []models.Transaction{}
		}
		writeJSON(w, txs)
	}
}

type txEdit struct {
	ID           int64  `json:"id"`
	Category     string `json:"category"`
	Direction    string `json:"direction"`
	Purpose      string `json:"purpose"`
	Counterparty string `json:"counterparty"`
}

// handleEditTransactions применяет ручные правки операций и пересчитывает анализ (детерминированно, без ИИ).
func handleEditTransactions(store *postgres.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if store == nil {
			httpError(w, http.StatusServiceUnavailable, "нет БД")
			return
		}
		userID, ok := UserIDFromContext(r.Context())
		if !ok || userID == "" {
			httpError(w, http.StatusUnauthorized, "не авторизован")
			return
		}
		uploadID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil || uploadID <= 0 {
			httpError(w, http.StatusBadRequest, "некорректный id загрузки")
			return
		}
		owns, err := userOwnsUpload(store, r.Context(), userID, uploadID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "не удалось проверить доступ")
			return
		}
		if !owns {
			httpError(w, http.StatusNotFound, "загрузка не найдена")
			return
		}
		var req struct {
			Edits []txEdit `json:"edits"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpError(w, http.StatusBadRequest, "некорректный JSON")
			return
		}
		for _, e := range req.Edits {
			dir := e.Direction
			if dir != string(models.In) && dir != string(models.Out) {
				dir = string(models.Out)
			}
			_ = store.UpdateTransactionFields(r.Context(), uploadID, e.ID, e.Category, dir, e.Purpose, e.Counterparty)
		}

		txs, err := store.GetTransactionsByUpload(r.Context(), uploadID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "не удалось прочитать транзакции")
			return
		}
		profile := taxProfileForUser(r.Context(), store, userID)
		res := metrics.ComputeAudit(txs)
		res.TaxRegime = profile.TaxRegime
		res.Checks = checks.Run(res, txs, profile)
		var planned []models.PlannedPayment
		if store != nil {
			planned, _ = store.ListPlanned(r.Context(), userID)
		}
		res.Forecast = forecast.Build(txs, res.ClosingBalance, res.Period.To, 60, planned)
		cpClient2, _ := counterparty.NewClientFromEnv()
		res.Compliance = compliance.Run(res, txs, counterparty.CollectStatuses(r.Context(), cpClient2, txs), profile)
		res.Scenarios = simulate.Scenarios(res, txs)
		res.Rating = rating.Compute(res)
		res.UploadID = uploadID
		if prev, e := store.GetAuditResult(r.Context(), uploadID); e == nil {
			res.Summary = prev.Summary
			res.Recommendations = prev.Recommendations
		}
		_ = store.SaveAuditResult(r.Context(), uploadID, res)
		writeJSON(w, res)
	}
}

// handleSimulate — what-if песочница: применяет действие к транзакциям загрузки и возвращает «было/стало».
func handleSimulate(store *postgres.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if store == nil {
			httpError(w, http.StatusServiceUnavailable, "нет БД")
			return
		}
		userID, ok := UserIDFromContext(r.Context())
		if !ok || userID == "" {
			httpError(w, http.StatusUnauthorized, "не авторизован")
			return
		}
		uploadID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil || uploadID <= 0 {
			httpError(w, http.StatusBadRequest, "некорректный id загрузки")
			return
		}
		owns, err := userOwnsUpload(store, r.Context(), userID, uploadID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "не удалось проверить доступ")
			return
		}
		if !owns {
			httpError(w, http.StatusNotFound, "загрузка не найдена")
			return
		}
		var act models.SimAction
		if err := json.NewDecoder(r.Body).Decode(&act); err != nil {
			httpError(w, http.StatusBadRequest, "некорректный JSON")
			return
		}
		txs, err := store.GetTransactionsByUpload(r.Context(), uploadID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "не удалось прочитать транзакции")
			return
		}
		before := simulate.Summarize(metrics.ComputeAudit(txs))
		after := simulate.Summarize(metrics.ComputeAudit(simulate.Apply(txs, act)))
		writeJSON(w, map[string]any{"before": before, "after": after})
	}
}

func saveUploadedFile(src multipart.File, originalName string) (storedPath string, checksum string, size int64, err error) {
	defer src.Close()

	uploadID := fmt.Sprintf("%d", time.Now().UnixNano())

	safeName := filepath.Base(originalName)
	safeName = strings.ReplaceAll(safeName, " ", "_")
	if safeName == "" {
		safeName = "upload.bin"
	}

	baseDir := os.Getenv("UPLOADS_DIR")
	if baseDir == "" {
		baseDir = "/data/uploads" // прод: docker volume; локально может быть недоступен
	}
	dir := filepath.Join(baseDir, uploadID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		// локальная разработка без тома /data — падаем в temp
		dir = filepath.Join(os.TempDir(), "finaudit-uploads", uploadID)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", "", 0, err
		}
	}

	storedPath = filepath.Join(dir, safeName)

	dst, err := os.Create(storedPath)
	if err != nil {
		return "", "", 0, err
	}
	defer dst.Close()

	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(dst, h), src)
	if err != nil {
		return "", "", 0, err
	}

	checksum = hex.EncodeToString(h.Sum(nil))
	size = n
	return storedPath, checksum, size, nil
}
