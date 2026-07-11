package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/wwpp/finaudit/internal/categorize"
	"github.com/wwpp/finaudit/internal/checks"
	"github.com/wwpp/finaudit/internal/compliance"
	"github.com/wwpp/finaudit/internal/config"
	"github.com/wwpp/finaudit/internal/counterparty"
	"github.com/wwpp/finaudit/internal/crypto"
	"github.com/wwpp/finaudit/internal/forecast"
	"github.com/wwpp/finaudit/internal/ingest"
	"github.com/wwpp/finaudit/internal/llm"
	"github.com/wwpp/finaudit/internal/metrics"
	"github.com/wwpp/finaudit/internal/models"
	"github.com/wwpp/finaudit/internal/rating"
	"github.com/wwpp/finaudit/internal/simulate"
	"github.com/wwpp/finaudit/internal/storage/postgres"
)

type docAPI struct {
	store  *postgres.Store
	cfg    *config.Config
	logger *slog.Logger
	cipher *crypto.Cipher
}

func newDocAPI(store *postgres.Store, cfg *config.Config, logger *slog.Logger, cipher *crypto.Cipher) *docAPI {
	return &docAPI{store: store, cfg: cfg, logger: logger, cipher: cipher}
}

func (a *docAPI) registerRoutes(r chi.Router) {
	r.Post("/api/documents", a.handleUploadDocuments())
	r.Get("/api/documents", a.handleListDocuments())
	r.Post("/api/documents/{id}/analyze", a.handleAnalyzeDocument())
	r.Delete("/api/documents/{id}", a.handleDeleteDocument())
}

// enrichWithUserKey добавляет выводы LLM через активный ключ юзера (с фолбэком на глобальный).
func enrichWithUserKey(store *postgres.Store, cfg *config.Config, cipher *crypto.Cipher, ctx context.Context, uid int64, res *models.AuditResult) {
	client := llm.New(cfg)
	if cipher != nil {
		if provider, model, keyEnc, err := store.ActiveCredentialSecret(ctx, uid); err == nil {
			if key, err2 := cipher.Decrypt(keyEnc); err2 == nil {
				base := ""
				if p, ok := llm.GetProvider(provider); ok {
					base = p.BaseURL
				}
				if tokens, err3 := client.EnrichWith(ctx, res, base, string(key), model); err3 == nil {
					_ = store.AddTokens(ctx, uid, tokens)
					return
				}
			}
		}
	}
	_ = client.Enrich(ctx, res)
}

func (a *docAPI) handleUploadDocuments() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, uidStr, ok := batchUserID(w, r)
		if !ok {
			return
		}
		if a.store == nil {
			httpError(w, http.StatusServiceUnavailable, "хранилище недоступно")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBatchBytes)
		if err := r.ParseMultipartForm(40 << 20); err != nil {
			httpError(w, http.StatusBadRequest, "файлы не прочитаны или слишком большие (лимит 60 МБ)")
			return
		}
		fhs := r.MultipartForm.File["files"]
		if len(fhs) == 0 {
			httpError(w, http.StatusBadRequest, "файлы не переданы (ожидается поле 'files')")
			return
		}
		for _, fh := range fhs {
			if !allowedUpload(fh.Filename) {
				continue // пропускаем неподдерживаемые типы
			}
			f, err := fh.Open()
			if err != nil {
				continue
			}
			path, _, size, err := saveUploadedFile(f, fh.Filename)
			if err != nil {
				a.logger.Warn("doc: save file", "err", err)
				continue
			}
			if _, err := a.store.CreateDocument(r.Context(), uidStr, fh.Filename, path, size); err != nil {
				a.logger.Warn("doc: create document", "err", err)
			}
		}
		docs, err := a.store.ListDocuments(r.Context(), uidStr)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "ошибка чтения документов")
			return
		}
		if docs == nil {
			docs = []postgres.Document{}
		}
		writeJSON(w, docs)
	}
}

func (a *docAPI) handleListDocuments() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, uidStr, ok := batchUserID(w, r)
		if !ok {
			return
		}
		docs, err := a.store.ListDocuments(r.Context(), uidStr)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "ошибка чтения документов")
			return
		}
		if docs == nil {
			docs = []postgres.Document{}
		}
		writeJSON(w, docs)
	}
}

func (a *docAPI) handleAnalyzeDocument() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid, uidStr, ok := batchUserID(w, r)
		if !ok {
			return
		}
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			httpError(w, http.StatusBadRequest, "некорректный id")
			return
		}
		filename, path, err := a.store.DocumentPath(r.Context(), uidStr, id)
		if err != nil {
			httpError(w, http.StatusNotFound, "документ не найден")
			return
		}
		txs, err := ingest.ParseFile(path)
		if err != nil {
			httpError(w, http.StatusUnprocessableEntity, "не удалось распарсить документ: "+err.Error())
			return
		}
		txs = categorize.Categorize(txs)
		profile := taxProfileForUser(r.Context(), a.store, uidStr)

		uploadID, err := a.store.CreateUpload(r.Context(), uidStr, filename)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "не удалось создать анализ")
			return
		}
		if err := a.store.BulkInsertTransactions(r.Context(), uploadID, txs); err != nil {
			a.logger.Warn("doc: bulk insert", "err", err)
		}
		res := metrics.ComputeAudit(txs)
		res.TaxRegime = profile.TaxRegime
		res.Checks = checks.Run(res, txs, profile)
		planned, _ := a.store.ListPlanned(r.Context(), uidStr)
		res.Forecast = forecast.Build(txs, res.ClosingBalance, res.Period.To, 60, planned)
		cpClient, _ := counterparty.NewClientFromEnv()
		res.Compliance = compliance.Run(res, txs, counterparty.CollectStatuses(r.Context(), cpClient, txs), profile)
		res.Scenarios = simulate.Scenarios(res, txs)
		res.Rating = rating.Compute(res)
		res.UploadID = uploadID
		enrichWithUserKey(a.store, a.cfg, a.cipher, r.Context(), uid, &res)
		if err := a.store.SaveAuditResult(r.Context(), uploadID, res); err != nil {
			a.logger.Warn("doc: save audit", "err", err)
		}
		if err := a.store.SetDocumentUpload(r.Context(), id, uploadID); err != nil {
			a.logger.Warn("doc: link upload", "err", err)
		}
		writeJSON(w, map[string]int64{"uploadId": uploadID})
	}
}

func (a *docAPI) handleDeleteDocument() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, uidStr, ok := batchUserID(w, r)
		if !ok {
			return
		}
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			httpError(w, http.StatusBadRequest, "некорректный id")
			return
		}
		path, err := a.store.DeleteDocument(r.Context(), uidStr, id)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				httpError(w, http.StatusNotFound, "документ не найден")
				return
			}
			httpError(w, http.StatusInternalServerError, "не удалось удалить документ")
			return
		}
		_ = os.Remove(path) // best-effort удаление файла с диска
		writeJSON(w, map[string]bool{"ok": true})
	}
}
