package llm

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/emrah/mf-backend/internal/common"
	"github.com/go-chi/chi/v5"
)

// RunStore is the persistence behaviour these handlers need, declared here on
// the consuming side rather than exported from the store. *Store satisfies it
// implicitly, so nothing has to be registered or wired differently — but the
// handlers now depend on the four operations they actually call instead of on
// a concrete type carrying a live connection pool. That is what makes them
// testable and benchmarkable without a running PostgreSQL, which in turn is
// what lets the performance work here be defended by tests in CI.
type RunStore interface {
	CreateRun(ctx context.Context, userID string, req CreateRunRequest) (Run, error)
	GetRun(ctx context.Context, userID, runID string) (Run, error)
	ListRuns(ctx context.Context, userID, model string, limit int, before time.Time) (ListResult, error)
	DeleteRun(ctx context.Context, userID, runID string) error
	UpsertScore(ctx context.Context, sc Score) (Score, error)
	Metrics(ctx context.Context, userID string) (Metrics, error)
}

// Handler serves the LLM monitoring & decision-scoring endpoints.
type Handler struct {
	store RunStore
}

func NewHandler(store RunStore) *Handler { return &Handler{store: store} }

// browserModels are the WebLLM/MLC-LLM models the frontend can run in-browser.
// Gemma is the required model for this capstone; others are offered as options.
var browserModels = []ModelInfo{
	{ID: "gemma-2-2b-it-q4f16_1-MLC", Label: "Gemma 2 2B Instruct", Family: "gemma", SizeHint: "~1.4 GB", Recommended: true},
	{ID: "gemma-2-2b-it-q4f32_1-MLC", Label: "Gemma 2 2B Instruct (f32)", Family: "gemma", SizeHint: "~2.5 GB", Recommended: false},
	{ID: "Llama-3.2-1B-Instruct-q4f16_1-MLC", Label: "Llama 3.2 1B Instruct", Family: "llama", SizeHint: "~0.9 GB", Recommended: false},
	{ID: "Phi-3.5-mini-instruct-q4f16_1-MLC", Label: "Phi 3.5 Mini Instruct", Family: "phi", SizeHint: "~2.2 GB", Recommended: false},
}

// Models lists the browser-runnable models (public — the login screen may
// preview them). GET /llm/models
func (h *Handler) Models(w http.ResponseWriter, r *http.Request) {
	common.JSON(w, http.StatusOK, map[string]any{"models": browserModels})
}

// CreateRun records a raw LLM interaction. POST /llm/runs
func (h *Handler) CreateRun(w http.ResponseWriter, r *http.Request) {
	claims, _ := common.ClaimsFromContext(r.Context())
	var req CreateRunRequest
	if err := common.Decode(r, &req); err != nil {
		common.Error(w, err)
		return
	}
	if req.Model == "" || req.Prompt == "" {
		common.Error(w, common.ErrBadRequest("model and prompt are required"))
		return
	}

	run, err := h.store.CreateRun(r.Context(), claims.UserID, req)
	if err != nil {
		common.Error(w, common.ErrInternal("could not save run"))
		return
	}

	// Optionally score immediately so the dashboard is populated in one call.
	if req.AutoScore {
		score := ScoreRun(run, DefaultWeights())
		if saved, err := h.store.UpsertScore(r.Context(), score); err == nil {
			run.Score = &saved
		}
	}
	common.JSON(w, http.StatusCreated, run)
}

// ListRuns returns a page of the user's runs, newest first. GET /llm/runs
//
// Paging is by cursor: ?before=<RFC3339 timestamp> from the previous page's
// next_cursor. An absent cursor returns the newest page. The old ?offset= form
// is gone — its cost grew with depth, and a cursor is both cheaper and stable
// when rows are inserted while the user is paging.
func (h *Handler) ListRuns(w http.ResponseWriter, r *http.Request) {
	claims, _ := common.ClaimsFromContext(r.Context())
	q := r.URL.Query()
	limit := clampInt(atoiDefault(q.Get("limit"), 20), 1, 100)
	model := q.Get("model")

	var before time.Time
	if raw := q.Get("before"); raw != "" {
		parsed, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			common.Error(w, common.ErrBadRequest("before must be an RFC3339 timestamp"))
			return
		}
		before = parsed
	}

	result, err := h.store.ListRuns(r.Context(), claims.UserID, model, limit, before)
	if err != nil {
		common.Error(w, common.ErrInternal("could not list runs"))
		return
	}
	common.JSON(w, http.StatusOK, result)
}

// GetRun returns a single run with its score. GET /llm/runs/{id}
func (h *Handler) GetRun(w http.ResponseWriter, r *http.Request) {
	claims, _ := common.ClaimsFromContext(r.Context())
	run, err := h.store.GetRun(r.Context(), claims.UserID, chi.URLParam(r, "id"))
	if err != nil {
		common.Error(w, common.ErrNotFound("run not found"))
		return
	}
	common.JSON(w, http.StatusOK, run)
}

// DeleteRun removes a run. DELETE /llm/runs/{id}
func (h *Handler) DeleteRun(w http.ResponseWriter, r *http.Request) {
	claims, _ := common.ClaimsFromContext(r.Context())
	if err := h.store.DeleteRun(r.Context(), claims.UserID, chi.URLParam(r, "id")); err != nil {
		common.Error(w, common.ErrNotFound("run not found"))
		return
	}
	common.JSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ScoreRun computes (or recomputes) the decision score. POST /llm/runs/{id}/score
func (h *Handler) ScoreRun(w http.ResponseWriter, r *http.Request) {
	claims, _ := common.ClaimsFromContext(r.Context())

	// Body is optional; allow custom weights but tolerate an empty body.
	var req ScoreRequest
	if r.ContentLength > 0 {
		if err := common.Decode(r, &req); err != nil {
			common.Error(w, err)
			return
		}
	}

	run, err := h.store.GetRun(r.Context(), claims.UserID, chi.URLParam(r, "id"))
	if err != nil {
		common.Error(w, common.ErrNotFound("run not found"))
		return
	}

	weights := DefaultWeights()
	if req.Weights != nil {
		weights = *req.Weights
	}
	score := ScoreRun(run, weights)
	saved, err := h.store.UpsertScore(r.Context(), score)
	if err != nil {
		common.Error(w, common.ErrInternal("could not save score"))
		return
	}
	common.JSON(w, http.StatusOK, saved)
}

// GetScore returns the stored score for a run. GET /llm/runs/{id}/score
func (h *Handler) GetScore(w http.ResponseWriter, r *http.Request) {
	claims, _ := common.ClaimsFromContext(r.Context())
	// Ownership check via GetRun keeps users from reading others' scores.
	run, err := h.store.GetRun(r.Context(), claims.UserID, chi.URLParam(r, "id"))
	if err != nil {
		common.Error(w, common.ErrNotFound("run not found"))
		return
	}
	if run.Score == nil {
		common.Error(w, common.ErrNotFound("run has not been scored yet"))
		return
	}
	common.JSON(w, http.StatusOK, run.Score)
}

// Metrics returns the aggregate dashboard summary. GET /llm/metrics
func (h *Handler) Metrics(w http.ResponseWriter, r *http.Request) {
	claims, _ := common.ClaimsFromContext(r.Context())
	m, err := h.store.Metrics(r.Context(), claims.UserID)
	if err != nil {
		common.Error(w, common.ErrInternal("could not compute metrics"))
		return
	}
	common.JSON(w, http.StatusOK, m)
}

// ---- small helpers ----

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return def
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
