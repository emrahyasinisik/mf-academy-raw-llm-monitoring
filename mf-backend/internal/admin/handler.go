package admin

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/emrah/mf-backend/internal/auth"
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

// AccountStore is the account/org surface behind /admin/accounts. It is kept
// narrow because the handler must not grow into an impersonation or case-content
// reader while the panel only needs account metadata.
type AccountStore interface {
	ListAccounts(ctx context.Context, q AccountListQuery) (AccountListResult, error)
	CreateIndividual(ctx context.Context, name, email, hash string) (AccountSummary, AccountMember, error)
	CreateCompany(ctx context.Context, orgName, taxID string, seats int, ownerName, ownerEmail, hash string) (AccountSummary, AccountMember, error)
	GetAccount(ctx context.Context, id string) (AccountDetail, error)
	SuspendAccount(ctx context.Context, id string) error
	SetAccountStatus(ctx context.Context, id, status string) error
	DeleteAccount(ctx context.Context, id string) error
}

// AuditStore records and lists operator actions. detail must stay metadata-only.
type AuditStore interface {
	WriteAudit(ctx context.Context, actorID, action, target string, detail map[string]any) error
	ListAudit(ctx context.Context, page, limit int) (AuditListResult, error)
}

type StatsStore interface {
	Stats(ctx context.Context, from, to time.Time) (StatsResponse, error)
}

type ControlStore interface {
	AdapterStore
	AccountStore
	LegalStore
	AuditStore
	StatsStore
}

// AdapterSwapper is the live control plane of the hot-swap runtime.
//
// Declared here rather than imported as a concrete type for the usual reason —
// the handler can then be tested without a GPU box — but also because it is
// genuinely optional. A deployment with only the compiled engine passes nil,
// and activation degrades to switching model ids instead of failing.
type AdapterSwapper interface {
	Configured() bool
	// Activate applies exactly one adapter and returns how long the swap took.
	// An empty name serves the untuned base.
	Activate(ctx context.Context, name string) (time.Duration, error)
	// Active reports what the running engine actually has applied, which is not
	// always what the database intends — the engine forgets on restart.
	Active(ctx context.Context) (string, error)
}

// Handler serves the admin control plane.
type Handler struct {
	store      AdapterStore
	settings   SettingsStore
	mcp        MCPStore
	accounts   AccountStore
	legal      LegalStore
	audit      AuditStore
	stats      StatsStore
	runtime    AdapterSwapper
	metrics    MetricsQuerier
	bcryptCost int
}

func NewHandler(store ControlStore, set SettingsStore, mcp MCPStore, legal LegalStore, audit AuditStore, stats StatsStore, rt AdapterSwapper, mq MetricsQuerier, bcryptCost int) *Handler {
	if bcryptCost < auth.MinHashCost {
		bcryptCost = auth.MinHashCost
	}
	return &Handler{
		store:      store,
		settings:   set,
		mcp:        mcp,
		accounts:   store,
		legal:      legal,
		audit:      audit,
		stats:      stats,
		runtime:    rt,
		metrics:    mq,
		bcryptCost: bcryptCost,
	}
}

func (h *Handler) recordAudit(ctx context.Context, actorID, action, target string, detail map[string]any) {
	if h.audit == nil {
		return
	}
	if err := h.audit.WriteAudit(ctx, actorID, action, target, detail); err != nil {
		slog.Error("audit write failed", "action", action, "target", target, "error", err)
	}
}

// hotSwapReady reports whether a live swap can even be attempted. Written as a
// method so the nil interface and the nil-but-typed pointer both answer false;
// only one of those is caught by `h.runtime != nil`.
func (h *Handler) hotSwapReady() bool {
	return h.runtime != nil && h.runtime.Configured()
}

// ActivationResult is what the panel gets back from an activate call.
//
// It reports the swap separately from the settings because the two can disagree
// in a way the operator has to know about: the settings write always succeeds,
// while the live swap can fail because the adapter was never loaded. Folding
// them into one green tick would show "active" over an engine still serving the
// previous adapter.
type ActivationResult struct {
	Settings any `json:"settings"`
	// HotSwapped is true only if a running engine actually changed adapter.
	HotSwapped bool `json:"hot_swapped"`
	// SwapMs is the measured duration of that change. Reported because "instant"
	// is the claim being made, and a number is the only honest form of it.
	SwapMs int64 `json:"swap_ms"`
	// Note explains what happened in the operator's terms, including the case
	// where nothing was swapped and a rebuild or restart is the next step.
	Note string `json:"note"`
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
	fields := []string{}
	if patch.DefaultModel != nil {
		fields = append(fields, "default_model")
	}
	if patch.SystemPrompt != nil {
		fields = append(fields, "system_prompt")
	}
	if patch.Temperature != nil {
		fields = append(fields, "temperature")
	}
	if patch.TopP != nil {
		fields = append(fields, "top_p")
	}
	if patch.MaxTokens != nil {
		fields = append(fields, "max_tokens")
	}
	if patch.RetentionDays != nil {
		fields = append(fields, "retention_days")
	}
	h.recordAudit(r.Context(), claims.UserID, "settings.update", "llm_settings", map[string]any{
		"fields": fields,
	})
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
// There are two ways an adapter can become the one being served, and this
// endpoint drives whichever the build supports:
//
//	gguf_adapter set   the hot-swap runtime already holds the adapter's weights.
//	                   Activation is one request that changes a scale from 0 to
//	                   1, takes milliseconds, and needs no restart. This is the
//	                   spec's hot-swap, and it is real.
//
//	mlc_model_id set   the compiled engine has no runtime LoRA slot — its kernels
//	                   were generated before the adapter existed — so what
//	                   changes is which already-built model id gets asked for.
//	                   Instant here too, but the build behind it took minutes.
//
// A build can have both, in which case both are pointed at it and the runtime
// setting decides which one users land on.
//
// Order matters. Settings is written first because it is what generation reads;
// if a later step fails, the system is already serving the right model and only
// the panel's badge is stale. Doing it the other way round would show a green
// "active" badge over a model nobody is serving.
func (h *Handler) ActivateAdapter(w http.ResponseWriter, r *http.Request) {
	claims, _ := common.ClaimsFromContext(r.Context())
	id := chi.URLParam(r, "id")

	// Read the build first: which artefacts it has decides what activation even
	// means, and a missing row should 404 rather than half-apply.
	a, err := h.store.GetAdapter(r.Context(), id)
	if errors.Is(err, ErrNoRows) {
		common.Error(w, common.ErrNotFound("adapter not found"))
		return
	}
	if err != nil {
		common.Error(w, common.ErrInternal("could not read adapter"))
		return
	}

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

	res := h.swap(r.Context(), a.GGUFAdapter, s)
	slog.Info("adapter activated", "adapter", a.Name, "model", s.ActiveModelID,
		"gguf", a.GGUFAdapter, "hot_swapped", res.HotSwapped, "swap_ms", res.SwapMs,
		"user_id", claims.UserID)
	h.recordAudit(r.Context(), claims.UserID, "adapter.activate", id, map[string]any{
		"name": a.Name,
	})
	common.JSON(w, http.StatusOK, res)
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

	// An empty name zeroes every adapter in the runtime, which is what makes
	// this a real revert rather than a database-only one. Without it the panel
	// would say "base model" while the engine was still applying a fine-tune —
	// the exact state that makes a bad adapter impossible to roll back from.
	res := h.swap(r.Context(), "", s)
	slog.Info("adapter deactivated, serving base model",
		"hot_swapped", res.HotSwapped, "user_id", claims.UserID)
	h.recordAudit(r.Context(), claims.UserID, "adapter.deactivate", "", nil)
	common.JSON(w, http.StatusOK, res)
}

// swap performs the live half of activation and describes what happened.
//
// Never returns an error: by the time it runs, the settings write has already
// committed and the compiled path is correct. A failure here degrades one of
// two serving paths, so it is reported in the body — where the operator reads
// it and can act — rather than raised as a 500 that would wrongly imply nothing
// took effect.
func (h *Handler) swap(ctx context.Context, gguf string, s any) ActivationResult {
	res := ActivationResult{Settings: s}

	// The notes are Turkish because they are shown verbatim in the operator's
	// panel, which is Turkish. An English sentence in the middle of it reads as
	// a leaked internal string, and the operator stops trusting the rest.
	switch {
	case gguf == "" && h.hotSwapReady():
		// Deactivation, or a build that only has a compiled artefact. Either
		// way the runtime must be returned to the base, or it keeps applying
		// whatever was active before.
		d, err := h.runtime.Activate(ctx, "")
		if err != nil {
			res.Note = "Derlenmiş model değişti; hot-swap motoru temel modele döndürülemedi: " + err.Error()
			return res
		}
		res.HotSwapped, res.SwapMs = true, d.Milliseconds()
		res.Note = "Hot-swap motoru ayarsız temel modele döndürüldü."
	case gguf == "":
		res.Note = "Derlenmiş model değişti. Bu build'in GGUF çıktısı yok, bu yüzden canlı " +
			"geçiş yapılamaz — peft/build_gguf.sh ile yayınlarsan aktivasyon çalışan " +
			"motorda anında etkili olur."
	case !h.hotSwapReady():
		res.Note = "Derlenmiş model değişti. Hot-swap motoru yapılandırılmamış, " +
			"bu yüzden GGUF adapter uygulanmadı."
	default:
		d, err := h.runtime.Activate(ctx, gguf)
		if err != nil {
			// The overwhelmingly likely cause, and the one worth spelling out:
			// llama-server takes adapters as startup flags, so a file published
			// after it booted is invisible until it restarts.
			res.Note = "Derlenmiş model değişti, ancak canlı geçiş başarısız oldu: " + err.Error()
			return res
		}
		res.HotSwapped, res.SwapMs = true, d.Milliseconds()
		res.Note = "Adapter çalışan motora uygulandı; yeniden başlatma yok, yeniden derleme yok."
	}
	return res
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
