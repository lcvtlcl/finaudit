package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"

	"github.com/go-chi/chi/v5"

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

type batchAPI struct {
	store  *postgres.Store
	cfg    *config.Config
	logger *slog.Logger
	cipher *crypto.Cipher
}

func newBatchAPI(store *postgres.Store, cfg *config.Config, logger *slog.Logger, cipher *crypto.Cipher) *batchAPI {
	return &batchAPI{store: store, cfg: cfg, logger: logger, cipher: cipher}
}

func (a *batchAPI) registerRoutes(r chi.Router) {
	r.Post("/api/batches", a.handleCreateBatch())
	r.Get("/api/batches", a.handleListBatches())
	r.Get("/api/batches/{id}", a.handleGetBatch())
}

func batchUserID(w http.ResponseWriter, r *http.Request) (int64, string, bool) {
	uidStr, ok := UserIDFromContext(r.Context())
	if !ok {
		httpError(w, http.StatusUnauthorized, "не авторизован")
		return 0, "", false
	}
	uid, err := strconv.ParseInt(uidStr, 10, 64)
	if err != nil {
		httpError(w, http.StatusUnauthorized, "не авторизован")
		return 0, "", false
	}
	return uid, uidStr, true
}

func accLabel(acc string) string {
	if acc == "" {
		return "без счёта"
	}
	return acc
}

type batchFileResult struct {
	Filename   string `json:"filename"`
	AccountKey string `json:"accountKey"`
	Status     string `json:"status"`
	Note       string `json:"note"`
	TxCount    int    `json:"txCount"`
	UploadID   *int64 `json:"uploadId"`
}

type batchGroupResult struct {
	AccountKey string  `json:"accountKey"`
	UploadID   int64   `json:"uploadId"`
	FileCount  int     `json:"fileCount"`
	Income     float64 `json:"income"`
	Expense    float64 `json:"expense"`
	HasGap     bool    `json:"hasGap"`
}

type createBatchResponse struct {
	BatchID      int64              `json:"batchId"`
	Files        []batchFileResult  `json:"files"`
	Groups       []batchGroupResult `json:"groups"`
	Incompatible []string           `json:"incompatible"`
}

type parsedFile struct {
	filename string
	account  string
	status   string
	note     string
	txs      []models.Transaction
}

func (a *batchAPI) handleCreateBatch() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid, uidStr, ok := batchUserID(w, r)
		if !ok {
			return
		}
		if a.store == nil {
			httpError(w, http.StatusServiceUnavailable, "хранилище недоступно")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBatchBytes)
		if err := r.ParseMultipartForm(40 << 20); err != nil {
			httpError(w, http.StatusBadRequest, "файлы не прочитаны или слишком большие (лимит 60 МБ на пачку)")
			return
		}
		fhs := r.MultipartForm.File["files"]
		if len(fhs) == 0 {
			httpError(w, http.StatusBadRequest, "файлы не переданы (ожидается поле 'files')")
			return
		}

		var items []parsedFile
		for _, fh := range fhs {
			if !allowedUpload(fh.Filename) {
				items = append(items, parsedFile{fh.Filename, "", "failed", "неподдерживаемый тип файла", nil})
				continue
			}
			f, err := fh.Open()
			if err != nil {
				items = append(items, parsedFile{fh.Filename, "", "failed", "не удалось открыть файл", nil})
				continue
			}
			path, _, _, err := saveUploadedFile(f, fh.Filename)
			if err != nil {
				items = append(items, parsedFile{fh.Filename, "", "failed", "не удалось сохранить файл", nil})
				continue
			}
			txs, err := ingest.ParseFile(path)
			if err != nil {
				items = append(items, parsedFile{fh.Filename, "", "failed", "не распознан: " + err.Error(), nil})
				continue
			}
			txs = categorize.Categorize(txs)
			items = append(items, parsedFile{fh.Filename, ingest.AccountKey(path), "ok", "", txs})
		}

		// Группируем распознанные файлы по счёту.
		var groupsOrder []string
		groupTxs := map[string][]models.Transaction{}
		groupCount := map[string]int{}
		for _, it := range items {
			if it.status != "ok" {
				continue
			}
			if _, exists := groupTxs[it.account]; !exists {
				groupsOrder = append(groupsOrder, it.account)
			}
			groupTxs[it.account] = append(groupTxs[it.account], it.txs...)
			groupCount[it.account]++
		}

		batchID, err := a.store.CreateBatch(r.Context(), uid, "")
		if err != nil {
			httpError(w, http.StatusInternalServerError, "не удалось создать пакет")
			return
		}

		// Сводим и анализируем каждую группу отдельным upload'ом (виден в Истории).
		groupUpload := map[string]int64{}
		var groups []batchGroupResult
		for _, acc := range groupsOrder {
			merged := ingest.Deduplicate(groupTxs[acc])
			sort.Slice(merged, func(i, j int) bool { return merged[i].Date.Before(merged[j].Date) })
			name := fmt.Sprintf("Свод: %s (%d файл.)", accLabel(acc), groupCount[acc])
			uploadID, res, err := a.analyzeAndStore(r.Context(), uidStr, uid, name, merged)
			if err != nil {
				a.logger.Warn("batch: analyze group", "err", err)
				continue
			}
			groupUpload[acc] = uploadID
			groups = append(groups, batchGroupResult{
				AccountKey: acc,
				UploadID:   uploadID,
				FileCount:  groupCount[acc],
				Income:     res.TotalIncome,
				Expense:    res.TotalExpense,
				HasGap:     res.CashGap != nil || (res.Forecast != nil && res.Forecast.Gap != nil),
			})
		}

		// Пишем файлы пакета и собираем ответ.
		var files []batchFileResult
		for _, it := range items {
			var up *int64
			if it.status == "ok" {
				if id, exists := groupUpload[it.account]; exists {
					v := id
					up = &v
				}
			}
			if err := a.store.AddBatchFile(r.Context(), batchID, it.filename, it.account, up, len(it.txs), it.status, it.note); err != nil {
				a.logger.Warn("batch: add file", "err", err)
			}
			files = append(files, batchFileResult{
				Filename: it.filename, AccountKey: it.account, Status: it.status,
				Note: it.note, TxCount: len(it.txs), UploadID: up,
			})
		}

		// Несовместимости: разные счета не сводятся между собой.
		var incompatible []string
		for i := 0; i < len(groupsOrder); i++ {
			for j := i + 1; j < len(groupsOrder); j++ {
				incompatible = append(incompatible, fmt.Sprintf(
					"Файлы счёта «%s» не сводятся в общий анализ с файлами счёта «%s» — это разные счета.",
					accLabel(groupsOrder[i]), accLabel(groupsOrder[j])))
			}
		}

		writeJSON(w, createBatchResponse{BatchID: batchID, Files: files, Groups: groups, Incompatible: incompatible})
	}
}

// analyzeAndStore прогоняет свод через весь конвейер и сохраняет как upload (виден в Истории).
func (a *batchAPI) analyzeAndStore(ctx context.Context, uidStr string, uid int64, name string, txs []models.Transaction) (int64, models.AuditResult, error) {
	uploadID, err := a.store.CreateUpload(ctx, uidStr, name)
	if err != nil {
		return 0, models.AuditResult{}, err
	}
	if err := a.store.BulkInsertTransactions(ctx, uploadID, txs); err != nil {
		a.logger.Warn("batch: bulk insert", "err", err)
	}
	profile := taxProfileForUser(ctx, a.store, uidStr)
	res := metrics.ComputeAudit(txs)
	res.TaxRegime = profile.TaxRegime
	res.Checks = checks.Run(res, txs, profile)
	planned, _ := a.store.ListPlanned(ctx, uidStr)
	res.Forecast = forecast.Build(txs, res.ClosingBalance, res.Period.To, 60, planned)
	cpClient, _ := counterparty.NewClientFromEnv()
	res.Compliance = compliance.Run(res, txs, counterparty.CollectStatuses(ctx, cpClient, txs), profile)
	res.Scenarios = simulate.Scenarios(res, txs)
	res.Rating = rating.Compute(res)
	res.UploadID = uploadID
	a.enrich(ctx, uid, &res)
	if err := a.store.SaveAuditResult(ctx, uploadID, res); err != nil {
		a.logger.Warn("batch: save audit", "err", err)
	}
	return uploadID, res, nil
}

// enrich добавляет выводы LLM через активный ключ юзера (с фолбэком на глобальный).
func (a *batchAPI) enrich(ctx context.Context, uid int64, res *models.AuditResult) {
	client := llm.New(a.cfg)
	if a.cipher != nil {
		if provider, model, keyEnc, err := a.store.ActiveCredentialSecret(ctx, uid); err == nil {
			if key, err2 := a.cipher.Decrypt(keyEnc); err2 == nil {
				base := ""
				if p, ok := llm.GetProvider(provider); ok {
					base = p.BaseURL
				}
				if tokens, err3 := client.EnrichWith(ctx, res, base, string(key), model); err3 == nil {
					_ = a.store.AddTokens(ctx, uid, tokens)
					return
				}
			}
		}
	}
	_ = client.Enrich(ctx, res)
}

func (a *batchAPI) handleListBatches() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid, _, ok := batchUserID(w, r)
		if !ok {
			return
		}
		list, err := a.store.ListBatches(r.Context(), uid)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "ошибка чтения пакетов")
			return
		}
		if list == nil {
			list = []postgres.BatchSummary{}
		}
		writeJSON(w, list)
	}
}

func (a *batchAPI) handleGetBatch() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid, _, ok := batchUserID(w, r)
		if !ok {
			return
		}
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			httpError(w, http.StatusBadRequest, "некорректный id")
			return
		}
		owner, err := a.store.BatchOwner(r.Context(), id)
		if err != nil || owner != uid {
			httpError(w, http.StatusNotFound, "пакет не найден")
			return
		}
		files, err := a.store.GetBatchFiles(r.Context(), id)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "ошибка чтения пакета")
			return
		}
		if files == nil {
			files = []postgres.BatchFile{}
		}
		writeJSON(w, map[string]any{"id": id, "files": files})
	}
}
