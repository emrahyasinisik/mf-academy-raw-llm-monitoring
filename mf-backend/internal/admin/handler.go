package admin

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/emrah/mf-backend/internal/common"
	"github.com/emrah/mf-backend/internal/settings"
	"github.com/go-chi/chi/v5"
)

// AdapterStore is the persistence this handler needs, declared on the consuming
// side so the handlers can be exercised without a live PostgreSQL — the same
// pattern llm.RunStore already uses.
type AdapterStore interface {
	CreateAdapter(ctx context.Context, userID string, req CreateAdapterRequest) (Adapter, error)
	ListAdapters(ctx context.Context) ([]Adapter, error)
	GetAdapter(ctx context.Context, id string) (Adapter, error)
	UpdateStatus(ctx context.Context, id string, req UpdateStatusRequest) (Adapter, error)
	MarkActive(ctx context.Context, id string) error
	DeleteAdapter(ctx context.Context, id string) error
	Logs(ctx context.Context, limit int, before time.Time, target string) (LogPage, error)
	Overview(ctx context.Context) (Overview, error)
}

// MCPStore is the MCP register. Separate from AdapterStore rather than folded
// into it because the two describe unrelated things — one is builds of a model,
// the other is which servers may be reached — and a single fat interface would
// force every test double to implement both.
type MCPStore interface {
	ListMCPServers(ctx context.Context) ([]MCPServer, error)
	EnabledMCPServers(ctx context.Context, side string) ([]MCPServer, error)
	CreateMCPServer(ctx context.Context, userID string, req CreateMCPServerRequest) (MCPServer, error)
	UpdateMCPServer(ctx context.Context, id string, req UpdateMCPServerRequest) (MCPServer, error)
	DeleteMCPServer(ctx context.Context, id string) error
}

// SettingsStore is the settings behaviour the panel drives.
type SettingsStore interface {
	Get(ctx context.Context) (settings.Settings, error)
	Update(ctx context.Context, p settings.Patch, userID string) (settings.Settings, error)
	SetActiveAdapter(ctx context.Context, id *string, userID string) (settings.Settings, error)
}

// Handler serves the admin control plane.
type Handler struct {
	store    AdapterStore
	settings SettingsStore
	mcp      MCPStore
}

func NewHandler(store AdapterStore, set SettingsStore, mcp MCPStore) *Handler {
	return &Handler{store: store, settings: set, mcp: mcp}
}

// ---- settings ----

// Settings returns the current runtime settings. GET /admin/settings
func (h *Handler) Settings(w http.ResponseWriter, r *http.Request) {
	s, err := h.settings.Get(r.Context())
	if err != nil {
		common.Error(w, common.ErrInternal("could not read settings"))
		return
	}
	common.JSON(w, http.StatusOK, s)
}

// UpdateSettings applies a partial change. PATCH /admin/settings
//
// Partial by design: the panel has two separate forms over one row (the system
// prompt, and the sampling parameters), and saving one must not clobber the
// other with whatever the browser last rendered.
func (h *Handler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	claims, _ := common.ClaimsFromContext(r.Context())

	var patch settings.Patch
	if err := common.Decode(r, &patch); err != nil {
		common.Error(w, err)
		return
	}
	s, err := h.settings.Update(r.Context(), patch, claims.UserID)
	if err != nil {
		// A rejected field is the caller's to fix; anything else is ours.
		// Matched by type, so a database failure can never be reported back as
		// though the client had sent something wrong.
		var invalid *settings.ValidationError
		if errors.As(err, &invalid) {
			common.Error(w, common.ErrBadRequest(invalid.Error()))
			return
		}
		common.Error(w, common.ErrInternal("could not update settings"))
		return
	}
	slog.Info("settings updated", "user_id", claims.UserID)
	common.JSON(w, http.StatusOK, s)
}

// ---- adapters ----

// ListAdapters returns every build. GET /admin/adapters
func (h *Handler) ListAdapters(w http.ResponseWriter, r *http.Request) {
	items, err := h.store.ListAdapters(r.Context())
	if err != nil {
		common.Error(w, common.ErrInternal("could not list adapters"))
		return
	}
	common.JSON(w, http.StatusOK, map[string]any{"adapters": items, "count": len(items)})
}

// CreateAdapter registers a build. POST /admin/adapters
func (h *Handler) CreateAdapter(w http.ResponseWriter, r *http.Request) {
	claims, _ := common.ClaimsFromContext(r.Context())

	var req CreateAdapterRequest
	if err := common.Decode(r, &req); err != nil {
		common.Error(w, err)
		return
	}
	if err := req.Normalize(); err != nil {
		common.Error(w, common.ErrBadRequest(err.Error()))
		return
	}

	a, err := h.store.CreateAdapter(r.Context(), claims.UserID, req)
	if err != nil {
		// The name column is UNIQUE, and a duplicate is a user mistake worth a
		// specific message rather than a 500.
		if isUniqueViolation(err) {
			common.Error(w, common.ErrConflict("an adapter with that name already exists"))
			return
		}
		common.Error(w, common.ErrInternal("could not create adapter"))
		return
	}
	slog.Info("adapter registered", "adapter", a.Name, "user_id", claims.UserID)
	common.JSON(w, http.StatusCreated, a)
}

// GetAdapter returns one build. GET /admin/adapters/{id}
func (h *Handler) GetAdapter(w http.ResponseWriter, r *http.Request) {
	a, err := h.store.GetAdapter(r.Context(), chi.URLParam(r, "id"))
	if errors.Is(err, ErrNoRows) {
		common.Error(w, common.ErrNotFound("adapter not found"))
		return
	}
	if err != nil {
		common.Error(w, common.ErrInternal("could not read adapter"))
		return
	}
	common.JSON(w, http.StatusOK, a)
}

// UpdateAdapterStatus advances a build. PATCH /admin/adapters/{id}/status
//
// Called by the pipeline on the GPU box as it works, not by a human. It is
// mounted under the same admin gate as everything else, so the pipeline must
// hold an admin token — the alternative, a separate machine credential, is more
// moving parts than a single-operator system needs.
func (h *Handler) UpdateAdapterStatus(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req UpdateStatusRequest
	if err := common.Decode(r, &req); err != nil {
		common.Error(w, err)
		return
	}
	if req.Status == "" {
		common.Error(w, common.ErrBadRequest("status is required"))
		return
	}

	a, err := h.store.UpdateStatus(r.Context(), id, req)
	switch {
	case errors.Is(err, ErrNoRows):
		common.Error(w, common.ErrNotFound("adapter not found"))
		return
	case errors.Is(err, ErrBadTransition):
		common.Error(w, common.ErrConflict("that status is not reachable from the adapter's current state"))
		return
	case err != nil:
		common.Error(w, common.ErrInternal("could not update adapter"))
		return
	}
	slog.Info("adapter status", "adapter", a.Name, "status", a.Status, "error", a.LastError)
	common.JSON(w, http.StatusOK, a)
}

// ActivateAdapter points generation at a build. POST /admin/adapters/{id}/activate
//
// This is the "one-click adapter load" of the spec. What it does not do is swap
// weights in a running engine: MLC compiles a model ahead of time and exposes
// no runtime slot for a LoRA, so what is switched here is *which already-built
// model id the inference host is asked for*. The build that produced that id
// happened earlier and took minutes; this call is the cheap final step.
//
// Order matters. Settings is written first because it is what generation reads;
// if the second write fails, the system is already serving the right model and
// only the panel's badge is stale. Doing it the other way round would show a
// green "active" badge over a model nobody is serving.
func (h *Handler) ActivateAdapter(w http.ResponseWriter, r *http.Request) {
	claims, _ := common.ClaimsFromContext(r.Context())
	id := chi.URLParam(r, "id")

	s, err := h.settings.SetActiveAdapter(r.Context(), &id, claims.UserID)
	switch {
	case errors.Is(err, settings.ErrNotReady):
		common.Error(w, common.ErrConflict(
			"that adapter has not finished building; only a ready adapter can be activated"))
		return
	case err != nil:
		common.Error(w, common.ErrInternal("could not activate adapter"))
		return
	}

	if err := h.store.MarkActive(r.Context(), id); err != nil {
		// Deliberately not fatal — see the ordering note above.
		slog.Warn("adapter activated but status column not updated", "adapter_id", id, "error", err)
	}
	slog.Info("adapter activated", "adapter_id", id, "model", s.ActiveModelID, "user_id", claims.UserID)
	common.JSON(w, http.StatusOK, s)
}

// DeactivateAdapter returns generation to the untuned base model.
// POST /admin/adapters/deactivate
func (h *Handler) DeactivateAdapter(w http.ResponseWriter, r *http.Request) {
	claims, _ := common.ClaimsFromContext(r.Context())

	s, err := h.settings.SetActiveAdapter(r.Context(), nil, claims.UserID)
	if err != nil {
		common.Error(w, common.ErrInternal("could not deactivate adapter"))
		return
	}
	if err := h.store.MarkActive(r.Context(), ""); err != nil {
		slog.Warn("deactivated but status column not updated", "error", err)
	}
	slog.Info("adapter deactivated, serving base model", "user_id", claims.UserID)
	common.JSON(w, http.StatusOK, s)
}

// DeleteAdapter removes a build. DELETE /admin/adapters/{id}
func (h *Handler) DeleteAdapter(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.store.DeleteAdapter(r.Context(), id); err != nil {
		if errors.Is(err, ErrNoRows) {
			common.Error(w, common.ErrNotFound("adapter not found"))
			return
		}
		common.Error(w, common.ErrInternal("could not delete adapter"))
		return
	}
	common.JSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ---- monitoring ----

// Logs is the operator's live log monitor. GET /admin/logs
func (h *Handler) Logs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := clamp(atoiDefault(q.Get("limit"), 50), 1, 200)

	var before time.Time
	if raw := q.Get("before"); raw != "" {
		t, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			common.Error(w, common.ErrBadRequest("before must be an RFC3339 timestamp"))
			return
		}
		before = t
	}

	target := q.Get("target")
	if target != "" && target != "browser" && target != "server" {
		common.Error(w, common.ErrBadRequest("target must be 'browser' or 'server'"))
		return
	}

	page, err := h.store.Logs(r.Context(), limit, before, target)
	if err != nil {
		common.Error(w, common.ErrInternal("could not read logs"))
		return
	}
	common.JSON(w, http.StatusOK, page)
}

// Overview is the panel header. GET /admin/overview
func (h *Handler) Overview(w http.ResponseWriter, r *http.Request) {
	o, err := h.store.Overview(r.Context())
	if err != nil {
		common.Error(w, common.ErrInternal("could not read overview"))
		return
	}
	common.JSON(w, http.StatusOK, o)
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

func clamp(n, lo, hi int) int {
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}
