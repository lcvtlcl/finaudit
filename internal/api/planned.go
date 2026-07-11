package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/wwpp/finaudit/internal/models"
	"github.com/wwpp/finaudit/internal/storage/postgres"
)

type plannedAPI struct {
	store *postgres.Store
}

func newPlannedAPI(store *postgres.Store) *plannedAPI { return &plannedAPI{store: store} }

func (a *plannedAPI) registerRoutes(r chi.Router) {
	r.Get("/api/planned", a.handleList())
	r.Post("/api/planned", a.handleCreate())
	r.Delete("/api/planned/{id}", a.handleDelete())
}

func (a *plannedAPI) handleList() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, uidStr, ok := batchUserID(w, r)
		if !ok {
			return
		}
		list, err := a.store.ListPlanned(r.Context(), uidStr)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "ошибка чтения плана")
			return
		}
		if list == nil {
			list = []models.PlannedPayment{}
		}
		writeJSON(w, list)
	}
}

type plannedRequest struct {
	Date      string  `json:"date"`
	Amount    float64 `json:"amount"`
	Direction string  `json:"direction"`
	Purpose   string  `json:"purpose"`
}

func (a *plannedAPI) handleCreate() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, uidStr, ok := batchUserID(w, r)
		if !ok {
			return
		}
		var req plannedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpError(w, http.StatusBadRequest, "некорректный JSON")
			return
		}
		d, err := time.Parse("2006-01-02", strings.TrimSpace(req.Date))
		if err != nil {
			httpError(w, http.StatusBadRequest, "дата в формате ГГГГ-ММ-ДД")
			return
		}
		if req.Amount <= 0 {
			httpError(w, http.StatusBadRequest, "сумма должна быть больше 0")
			return
		}
		dir := models.Out
		if req.Direction == string(models.In) {
			dir = models.In
		}
		id, err := a.store.CreatePlanned(r.Context(), uidStr, models.PlannedPayment{
			Date: d, Amount: req.Amount, Direction: dir, Purpose: strings.TrimSpace(req.Purpose),
		})
		if err != nil {
			httpError(w, http.StatusInternalServerError, "не удалось сохранить платёж")
			return
		}
		writeJSON(w, map[string]int64{"id": id})
	}
}

func (a *plannedAPI) handleDelete() http.HandlerFunc {
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
		if err := a.store.DeletePlanned(r.Context(), uidStr, id); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				httpError(w, http.StatusNotFound, "не найдено")
				return
			}
			httpError(w, http.StatusInternalServerError, "не удалось удалить")
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	}
}
