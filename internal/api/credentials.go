package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/wwpp/finaudit/internal/crypto"
	"github.com/wwpp/finaudit/internal/llm"
	"github.com/wwpp/finaudit/internal/storage/postgres"
)

// credAPI обслуживает персональные ключи ИИ, per-user настройки и профиль.
type credAPI struct {
	store  *postgres.Store
	cipher *crypto.Cipher // nil, если APP_ENC_KEY не задан (персональные ключи выключены)
}

func newCredAPI(store *postgres.Store, cipher *crypto.Cipher) *credAPI {
	return &credAPI{store: store, cipher: cipher}
}

func (a *credAPI) registerRoutes(r chi.Router) {
	r.Get("/api/providers", a.handleProviders())
	r.Get("/api/ai/credentials", a.handleListCredentials())
	r.Get("/api/ai/balance", a.handleBalance())
	r.Post("/api/ai/credentials", a.handleAddCredential())
	r.Delete("/api/ai/credentials/{id}", a.handleDeleteCredential())
	r.Post("/api/ai/credentials/{id}/activate", a.handleActivateCredential())
	r.Post("/api/profile", a.handleUpdateProfile())
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func (a *credAPI) userID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	uidStr, ok := UserIDFromContext(r.Context())
	if !ok {
		httpError(w, http.StatusUnauthorized, "не авторизован")
		return 0, false
	}
	uid, err := strconv.ParseInt(uidStr, 10, 64)
	if err != nil {
		httpError(w, http.StatusUnauthorized, "не авторизован")
		return 0, false
	}
	return uid, true
}

func (a *credAPI) handleProviders() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"providers": llm.Providers()})
	}
}

func (a *credAPI) handleListCredentials() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid, ok := a.userID(w, r)
		if !ok {
			return
		}
		creds, err := a.store.ListCredentials(r.Context(), uid)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "ошибка чтения ключей")
			return
		}
		us, _ := a.store.GetUserSettings(r.Context(), uid)
		activeProvider := ""
		for _, c := range creds {
			if c.Active {
				activeProvider = c.Provider
			}
		}
		writeJSON(w, map[string]any{
			"credentials": creds,
			"tokensUsed":  us.TokensUsed,
			"costUsd":     llm.CostUSD(activeProvider, us.TokensUsed),
			"encEnabled":  a.cipher != nil,
		})
	}
}

// handleBalance — best-effort остаток баланса аккаунта активного провайдера.
func (a *credAPI) handleBalance() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid, ok := a.userID(w, r)
		if !ok {
			return
		}
		if a.cipher == nil {
			writeJSON(w, map[string]any{"available": false})
			return
		}
		provider, _, keyEnc, err := a.store.ActiveCredentialSecret(r.Context(), uid)
		if err != nil {
			writeJSON(w, map[string]any{"available": false})
			return
		}
		key, err := a.cipher.Decrypt(keyEnc)
		if err != nil {
			writeJSON(w, map[string]any{"available": false})
			return
		}
		amount, currency, okb := llm.FetchBalance(r.Context(), provider, string(key))
		writeJSON(w, map[string]any{
			"available": okb,
			"amount":    amount,
			"currency":  currency,
			"provider":  provider,
		})
	}
}

type addCredentialRequest struct {
	Provider string `json:"provider"`
	Label    string `json:"label"`
	Key      string `json:"key"`
	Model    string `json:"model"`
}

func (a *credAPI) handleAddCredential() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid, ok := a.userID(w, r)
		if !ok {
			return
		}
		if a.cipher == nil {
			httpError(w, http.StatusServiceUnavailable, "шифрование ключей не настроено (APP_ENC_KEY)")
			return
		}
		var req addCredentialRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpError(w, http.StatusBadRequest, "некорректный JSON")
			return
		}
		req.Provider = strings.TrimSpace(req.Provider)
		req.Label = strings.TrimSpace(req.Label)
		req.Key = strings.TrimSpace(req.Key)
		req.Model = strings.TrimSpace(req.Model)

		prov, known := llm.GetProvider(req.Provider)
		if !known {
			httpError(w, http.StatusBadRequest, "неизвестный провайдер")
			return
		}
		if req.Key == "" {
			httpError(w, http.StatusBadRequest, "пустой ключ")
			return
		}
		if req.Label == "" {
			req.Label = prov.Name
		}
		if req.Model == "" && len(prov.Models) > 0 {
			req.Model = prov.Models[0]
		}

		enc, err := a.cipher.Encrypt([]byte(req.Key))
		if err != nil {
			httpError(w, http.StatusInternalServerError, "ошибка шифрования ключа")
			return
		}
		hint := req.Key
		if len(hint) > 4 {
			hint = hint[len(hint)-4:]
		}
		id, err := a.store.CreateCredential(r.Context(), uid, req.Provider, req.Label, enc, hint, req.Model)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "ошибка сохранения ключа")
			return
		}
		// Первый добавленный ключ делаем активным по умолчанию.
		if creds, e := a.store.ListCredentials(r.Context(), uid); e == nil && len(creds) == 1 {
			_ = a.store.SetActiveCredential(r.Context(), uid, id)
		}
		writeJSON(w, map[string]any{"id": id})
	}
}

func (a *credAPI) handleDeleteCredential() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid, ok := a.userID(w, r)
		if !ok {
			return
		}
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			httpError(w, http.StatusBadRequest, "некорректный id")
			return
		}
		if err := a.store.DeleteCredential(r.Context(), uid, id); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				httpError(w, http.StatusNotFound, "ключ не найден")
				return
			}
			httpError(w, http.StatusInternalServerError, "ошибка удаления ключа")
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	}
}

func (a *credAPI) handleActivateCredential() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid, ok := a.userID(w, r)
		if !ok {
			return
		}
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			httpError(w, http.StatusBadRequest, "некорректный id")
			return
		}
		if err := a.store.SetActiveCredential(r.Context(), uid, id); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				httpError(w, http.StatusNotFound, "ключ не найден")
				return
			}
			httpError(w, http.StatusInternalServerError, "ошибка активации ключа")
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	}
}

type updateProfileRequest struct {
	Name      string `json:"name"`
	Company   string `json:"company"`
	LegalForm string `json:"legal_form"`
	TaxRegime string `json:"tax_regime"`
}

func (a *credAPI) handleUpdateProfile() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid, ok := a.userID(w, r)
		if !ok {
			return
		}
		var req updateProfileRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpError(w, http.StatusBadRequest, "некорректный JSON")
			return
		}
		if err := a.store.UpdateProfile(r.Context(), uid, strings.TrimSpace(req.Name), strings.TrimSpace(req.Company)); err != nil {
			httpError(w, http.StatusInternalServerError, "ошибка сохранения профиля")
			return
		}
		if err := a.store.UpdateTaxProfile(r.Context(), uid, normLegalForm(req.LegalForm), normTaxRegime(req.TaxRegime)); err != nil {
			httpError(w, http.StatusInternalServerError, "ошибка сохранения режима")
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	}
}
