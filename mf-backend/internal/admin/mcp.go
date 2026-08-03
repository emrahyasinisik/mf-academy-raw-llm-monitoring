package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/emrah/mf-backend/internal/common"
	"github.com/emrah/mf-backend/internal/llm"
	"github.com/emrah/mf-backend/internal/settings"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

// This file is the "which MCP servers may be used" half of the control plane.
//
// The list has to come from the server rather than from a constant in the
// frontend bundle. A bundled list cannot be changed without a redeploy, and —
// more importantly — cannot be relied on to have been obeyed: anything shipped
// to a browser is a suggestion to that browser. Keeping the register here means
// switching a server off actually switches it off.

// Kinds and sides.
const (
	KindInternal = "internal"
	KindExternal = "external"

	SideFrontend = "frontend"
	SideBackend  = "backend"
	SideBoth     = "both"
)

// MCPServer is one registered server.
type MCPServer struct {
	ID          string          `json:"id"`
	Slug        string          `json:"slug"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Kind        string          `json:"kind"`
	URL         string          `json:"url"`
	Transport   string          `json:"transport"`
	Side        string          `json:"side"`
	Enabled     bool            `json:"enabled"`
	Config      json.RawMessage `json:"config"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

// CreateMCPServerRequest registers an external server.
type CreateMCPServerRequest struct {
	Slug        string          `json:"slug"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	URL         string          `json:"url"`
	Transport   string          `json:"transport"`
	Side        string          `json:"side"`
	Enabled     bool            `json:"enabled"`
	Config      json.RawMessage `json:"config"`
}

// UpdateMCPServerRequest is a partial change. Pointers so "not supplied" and
// "supplied as false" stay distinguishable — without that, a request toggling
// only the name would silently disable the server.
type UpdateMCPServerRequest struct {
	Name        *string         `json:"name"`
	Description *string         `json:"description"`
	URL         *string         `json:"url"`
	Side        *string         `json:"side"`
	Enabled     *bool           `json:"enabled"`
	Config      json.RawMessage `json:"config"`
}

var validTransports = map[string]bool{"http": true, "sse": true, "stdio": true}
var validSides = map[string]bool{SideFrontend: true, SideBackend: true, SideBoth: true}

// Normalize fills defaults and rejects what cannot be connected to.
func (r *CreateMCPServerRequest) Normalize() error {
	r.Slug = strings.TrimSpace(strings.ToLower(r.Slug))
	r.Name = strings.TrimSpace(r.Name)
	r.URL = strings.TrimSpace(r.URL)

	if r.Slug == "" {
		return fmt.Errorf("slug is required")
	}
	for _, c := range r.Slug {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return fmt.Errorf("slug may only contain lowercase letters, digits and hyphens")
		}
	}
	if r.Name == "" {
		r.Name = r.Slug
	}
	if r.Transport == "" {
		r.Transport = "http"
	}
	if !validTransports[r.Transport] {
		return fmt.Errorf("transport must be one of http, sse, stdio")
	}
	if r.Side == "" {
		r.Side = SideFrontend
	}
	if !validSides[r.Side] {
		return fmt.Errorf("side must be one of frontend, backend, both")
	}
	if len(r.Config) == 0 {
		r.Config = json.RawMessage("{}")
	}

	if r.URL == "" {
		return fmt.Errorf("url is required for an external server")
	}
	u, err := url.Parse(r.URL)
	if err != nil || u.Host == "" {
		return fmt.Errorf("url is not a valid absolute URL")
	}
	// A browser will refuse a plain-http subresource from an https page, so an
	// http server offered to the frontend is one that can only fail there.
	// Localhost is exempt: browsers treat it as a secure context, and a
	// developer running a server on their own machine is the common case.
	if u.Scheme != "https" && r.Side != SideBackend && !isLoopback(u.Hostname()) {
		return fmt.Errorf("a frontend-facing server must use https (or be on localhost); " +
			"a browser on an https page will block a plain-http connection")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("url scheme must be http or https")
	}
	return nil
}

func isLoopback(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// ---- store ----

const mcpColumns = `
	id, slug, name, description, kind, url, transport, side, enabled,
	config, created_at, updated_at`

func scanMCP(row pgx.Row) (MCPServer, error) {
	var s MCPServer
	var cfg []byte
	err := row.Scan(&s.ID, &s.Slug, &s.Name, &s.Description, &s.Kind, &s.URL,
		&s.Transport, &s.Side, &s.Enabled, &cfg, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return MCPServer{}, err
	}
	if len(cfg) == 0 {
		cfg = []byte("{}")
	}
	s.Config = cfg
	return s, nil
}

// ListMCPServers returns every registered server, internal first.
func (s *Store) ListMCPServers(ctx context.Context) ([]MCPServer, error) {
	rows, err := s.db.Query(ctx,
		`SELECT`+mcpColumns+` FROM mcp_servers
		  ORDER BY kind = 'external', name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []MCPServer{}
	for rows.Next() {
		m, err := scanMCP(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// EnabledMCPServers returns what a given side may connect to.
func (s *Store) EnabledMCPServers(ctx context.Context, side string) ([]MCPServer, error) {
	rows, err := s.db.Query(ctx,
		`SELECT`+mcpColumns+` FROM mcp_servers
		  WHERE enabled AND (side = $1 OR side = 'both')
		  ORDER BY kind = 'external', name`, side)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []MCPServer{}
	for rows.Next() {
		m, err := scanMCP(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// CreateMCPServer registers an external server.
func (s *Store) CreateMCPServer(ctx context.Context, userID string, req CreateMCPServerRequest) (MCPServer, error) {
	return scanMCP(s.db.QueryRow(ctx,
		`INSERT INTO mcp_servers (slug, name, description, kind, url, transport, side, enabled, config, created_by)
		 VALUES ($1,$2,$3,'external',$4,$5,$6,$7,$8,$9)
		 RETURNING`+mcpColumns,
		req.Slug, req.Name, req.Description, req.URL, req.Transport,
		req.Side, req.Enabled, req.Config, userID))
}

// UpdateMCPServer applies a partial change.
//
// The internal server's URL and kind are not editable: its address is a fact
// about the deployment rather than a setting, and letting an operator point it
// somewhere else would mean the panel showing "this service's own MCP server"
// next to somebody else's address.
func (s *Store) UpdateMCPServer(ctx context.Context, id string, req UpdateMCPServerRequest) (MCPServer, error) {
	var cfg any
	if len(req.Config) > 0 {
		cfg = []byte(req.Config)
	}
	m, err := scanMCP(s.db.QueryRow(ctx,
		`UPDATE mcp_servers SET
		     name        = COALESCE($2, name),
		     description = COALESCE($3, description),
		     url         = CASE WHEN kind = 'internal' THEN url ELSE COALESCE($4, url) END,
		     side        = COALESCE($5, side),
		     enabled     = COALESCE($6, enabled),
		     config      = COALESCE($7::jsonb, config),
		     updated_at  = now()
		 WHERE id = $1
		 RETURNING`+mcpColumns,
		id, req.Name, req.Description, req.URL, req.Side, req.Enabled, cfg))
	if errors.Is(err, pgx.ErrNoRows) {
		return MCPServer{}, ErrNoRows
	}
	return m, err
}

// DeleteMCPServer removes an external server. The internal one is refused: it
// describes the running service, so deleting it would not stop the endpoint
// existing — it would only stop clients being told about it.
func (s *Store) DeleteMCPServer(ctx context.Context, id string) error {
	tag, err := s.db.Exec(ctx, `DELETE FROM mcp_servers WHERE id = $1 AND kind = 'external'`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNoRows
	}
	return nil
}

// ---- handlers ----

// ListMCPServers returns the register. GET /admin/mcp-servers
func (h *Handler) ListMCPServers(w http.ResponseWriter, r *http.Request) {
	items, err := h.mcp.ListMCPServers(r.Context())
	if err != nil {
		common.Error(w, common.ErrInternal("could not list MCP servers"))
		return
	}
	common.JSON(w, http.StatusOK, map[string]any{"servers": items, "count": len(items)})
}

// CreateMCPServer registers one. POST /admin/mcp-servers
func (h *Handler) CreateMCPServer(w http.ResponseWriter, r *http.Request) {
	claims, _ := common.ClaimsFromContext(r.Context())

	var req CreateMCPServerRequest
	if err := common.Decode(r, &req); err != nil {
		common.Error(w, err)
		return
	}
	if err := req.Normalize(); err != nil {
		common.Error(w, common.ErrBadRequest(err.Error()))
		return
	}

	m, err := h.mcp.CreateMCPServer(r.Context(), claims.UserID, req)
	if err != nil {
		if isUniqueViolation(err) {
			common.Error(w, common.ErrConflict("a server with that slug already exists"))
			return
		}
		common.Error(w, common.ErrInternal("could not register the server"))
		return
	}
	slog.Info("mcp server registered", "slug", m.Slug, "side", m.Side, "user_id", claims.UserID)
	common.JSON(w, http.StatusCreated, m)
}

// UpdateMCPServer toggles or edits one. PATCH /admin/mcp-servers/{id}
func (h *Handler) UpdateMCPServer(w http.ResponseWriter, r *http.Request) {
	var req UpdateMCPServerRequest
	if err := common.Decode(r, &req); err != nil {
		common.Error(w, err)
		return
	}
	if req.Side != nil && !validSides[*req.Side] {
		common.Error(w, common.ErrBadRequest("side must be one of frontend, backend, both"))
		return
	}

	m, err := h.mcp.UpdateMCPServer(r.Context(), chi.URLParam(r, "id"), req)
	if errors.Is(err, ErrNoRows) {
		common.Error(w, common.ErrNotFound("no such MCP server"))
		return
	}
	if err != nil {
		common.Error(w, common.ErrInternal("could not update the server"))
		return
	}
	slog.Info("mcp server updated", "slug", m.Slug, "enabled", m.Enabled)
	common.JSON(w, http.StatusOK, m)
}

// DeleteMCPServer removes one. DELETE /admin/mcp-servers/{id}
func (h *Handler) DeleteMCPServer(w http.ResponseWriter, r *http.Request) {
	err := h.mcp.DeleteMCPServer(r.Context(), chi.URLParam(r, "id"))
	if errors.Is(err, ErrNoRows) {
		common.Error(w, common.ErrNotFound(
			"no such external MCP server; the built-in one cannot be deleted"))
		return
	}
	if err != nil {
		common.Error(w, common.ErrInternal("could not delete the server"))
		return
	}
	common.JSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ---- model choice ----

// ModelChoice is one option in the panel's model picker.
type ModelChoice struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Source string `json:"source"` // "builtin" or "adapter"
	// Available reports whether it can actually be served right now. A build
	// still compiling appears in the list — an operator wants to see it coming
	// — but must not be selectable, or the panel offers a model that answers
	// every request with a 404 from the inference host.
	Available bool   `json:"available"`
	Status    string `json:"status,omitempty"`
	Note      string `json:"note,omitempty"`
}

// Models lists what can be served. GET /admin/models
//
// Two sources merged into one list, because the distinction matters to the
// operator but the choice does not: a built-in model and an adapter build are
// both just a model id the inference host is asked for.
func (h *Handler) Models(w http.ResponseWriter, r *http.Request) {
	out := []ModelChoice{}
	for _, m := range llm.Catalogue() {
		// Only the server-runnable ones. A browser-only entry in a picker that
		// selects what the *backend* generates with would be a trap.
		if !slices.Contains(m.Targets, llm.TargetServer) {
			continue
		}
		out = append(out, ModelChoice{
			ID: m.ID, Label: m.Label, Source: "builtin", Available: true,
		})
	}

	adapters, err := h.store.ListAdapters(r.Context())
	if err != nil {
		common.Error(w, common.ErrInternal("could not list adapter builds"))
		return
	}
	for _, a := range adapters {
		ready := (a.Status == StatusReady || a.Status == StatusActive) && a.MLCModelID != ""
		note := ""
		if !ready {
			note = "henüz servis edilemez: build " + a.Status
		}
		id := a.MLCModelID
		if id == "" {
			id = a.Name
		}
		out = append(out, ModelChoice{
			ID: id, Label: a.Name + " (adapter)", Source: "adapter",
			Available: ready, Status: a.Status, Note: note,
		})
	}

	current, err := h.settings.Get(r.Context())
	if err != nil {
		common.Error(w, common.ErrInternal("could not read settings"))
		return
	}
	common.JSON(w, http.StatusOK, map[string]any{
		"models": out,
		"count":  len(out),
		// What is actually in force, resolved the same way the analysis path
		// resolves it. Returned so the panel shows the effective model rather
		// than making the reader replay the precedence rules in their head.
		"selected": map[string]any{
			"default_model":       current.DefaultModel,
			"active_adapter_id":   current.ActiveAdapterID,
			"active_adapter_name": current.ActiveAdapterName,
			"effective":           effectiveModel(current),
		},
	})
}

// effectiveModel mirrors the precedence in analysis.Handler.runOne.
//
// Duplicated deliberately and kept adjacent to a comment saying so: the panel
// must show what will actually run, and a panel that disagrees with the engine
// is worse than one that shows nothing. If the order changes there, it changes
// here — the alternative was exporting the resolution from analysis and making
// admin depend on it for one string.
func effectiveModel(s settings.Settings) string {
	if s.DefaultModel != "" {
		return s.DefaultModel
	}
	if s.ActiveModelID != "" {
		return s.ActiveModelID
	}
	return "qwen3-4b-instruct-q4f16_1-MLC"
}

// ClientServers is the non-admin view: what this caller's side is allowed to
// connect to. GET /mcp-servers (authenticated, any role)
//
// Separate from the admin listing rather than the same handler with a filter,
// because the two answer different questions and must not leak into each
// other. The admin list includes disabled servers, their config blobs and who
// added them; this one returns only what an ordinary client needs to open a
// connection. A config blob can hold an API key for the upstream server, and
// that is not something to hand to every logged-in browser.
func (h *Handler) ClientServers(w http.ResponseWriter, r *http.Request) {
	side := r.URL.Query().Get("side")
	if side == "" {
		side = SideFrontend
	}
	if !validSides[side] {
		common.Error(w, common.ErrBadRequest("side must be one of frontend, backend, both"))
		return
	}

	items, err := h.mcp.EnabledMCPServers(r.Context(), side)
	if err != nil {
		common.Error(w, common.ErrInternal("could not list MCP servers"))
		return
	}

	type clientView struct {
		Slug        string `json:"slug"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Kind        string `json:"kind"`
		URL         string `json:"url"`
		Transport   string `json:"transport"`
	}
	out := make([]clientView, 0, len(items))
	for _, m := range items {
		out = append(out, clientView{m.Slug, m.Name, m.Description, m.Kind, m.URL, m.Transport})
	}
	common.JSON(w, http.StatusOK, map[string]any{"servers": out, "count": len(out)})
}
