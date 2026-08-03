package wiki

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/emrah/mf-backend/internal/common"
	"github.com/emrah/mf-backend/internal/llm"
	"github.com/emrah/mf-backend/internal/settings"
	"github.com/go-chi/chi/v5"
)

// DocumentStore is the persistence this handler needs, declared on the consumer
// side so the handlers can be exercised without a live PostgreSQL.
type DocumentStore interface {
	Ingest(ctx context.Context, userID string, d Document) (Document, error)
	Get(ctx context.Context, slug string) (Document, error)
	List(ctx context.Context) ([]Document, error)
	Delete(ctx context.Context, slug string) error
	Search(ctx context.Context, query string, limit int) ([]Hit, error)
}

// SettingsSource supplies the operator's model choice. Read per request, so the
// panel's changes take effect without redeploying a service whose model runs on
// somebody's desktop.
type SettingsSource interface {
	Get(ctx context.Context) (settings.Settings, error)
}

// Handler serves DeepKwiki.
type Handler struct {
	store DocumentStore
	gen   llm.Generator
	set   SettingsSource
}

func NewHandler(store DocumentStore, gen llm.Generator, set SettingsSource) *Handler {
	return &Handler{store: store, gen: gen, set: set}
}

// defaultModel is the fallback when neither the request nor the settings name
// one. Duplicated from the analysis package rather than shared, because these
// two features are allowed to diverge on which model suits them.
const defaultModel = "qwen3-4b-instruct-q4f16_1-MLC"

// answerTemperature is pinned low and is not the operator's chat temperature.
// Summarising supplied passages is an extraction task; sampling variety here
// shows up as invented detail, which is the exact failure this feature exists
// to prevent.
const answerTemperature = 0.1

// answerMaxTokens bounds the reply. The prompt asks for at most six sentences,
// and this is the enforcement — without it a model that ignores the instruction
// runs until the request deadline on a card that serves one request at a time.
const answerMaxTokens = 700

const (
	minQueryChars   = 2
	maxQueryChars   = 500
	maxBodyChars    = 400_000
	maxTitleChars   = 200
	defaultSearchN  = 8
	maxDocumentTags = 12
)

// ---- read paths ----

// Search returns matching passages. GET /wiki/search?q=...&limit=n
func (h *Handler) Search(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len([]rune(q)) < minQueryChars {
		common.Error(w, common.ErrBadRequest("q must be at least 2 characters"))
		return
	}
	if len([]rune(q)) > maxQueryChars {
		common.Error(w, common.ErrBadRequest("q is too long"))
		return
	}

	limit := defaultSearchN
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 {
		limit = n
	}

	hits, err := h.store.Search(r.Context(), q, limit)
	if err != nil {
		slog.Error("wiki search failed", "error", err)
		common.Error(w, common.ErrInternal("search failed"))
		return
	}

	common.JSON(w, http.StatusOK, map[string]any{
		"query": q,
		"hits":  hits,
		"count": len(hits),
	})
}

// AskRequest is a grounded question.
type AskRequest struct {
	Query string `json:"query"`
	Model string `json:"model"`
}

// Ask answers from the knowledge base. POST /wiki/ask
//
// Retrieval happens first and the model is only called if something was found.
// That ordering is the whole feature: with no passages there is nothing to
// ground an answer in, and asking anyway would get a fluent reply drawn from
// the model's own training — indistinguishable, to a reader, from a sourced one.
func (h *Handler) Ask(w http.ResponseWriter, r *http.Request) {
	var req AskRequest
	if err := common.Decode(r, &req); err != nil {
		common.Error(w, common.ErrBadRequest("invalid JSON body"))
		return
	}

	req.Query = strings.TrimSpace(req.Query)
	if n := len([]rune(req.Query)); n < minQueryChars || n > maxQueryChars {
		common.Error(w, common.ErrBadRequest("query must be between 2 and 500 characters"))
		return
	}

	ans, err := h.Answer(r.Context(), req)
	if err != nil {
		// An *APIError carries a status and a message chosen for the reader —
		// "the inference host is switched off" is not an internal error and
		// must not be flattened into one. Anything else is ours.
		var apiErr *common.APIError
		if errors.As(err, &apiErr) {
			common.Error(w, apiErr)
			return
		}
		slog.Error("wiki ask failed", "error", err)
		common.Error(w, common.ErrInternal("could not answer"))
		return
	}
	common.JSON(w, http.StatusOK, ans)
}

// Lookup is the retrieval service surface, used by the MCP tool. Named
// differently from the Search HTTP handler because they are different things —
// one speaks JSON over a socket, the other returns passages — and giving both
// the obvious name would mean neither could be called from the other.
func (h *Handler) Lookup(ctx context.Context, query string, limit int) ([]Hit, error) {
	return h.store.Search(ctx, query, limit)
}

// Answer is the exported service surface, used by both the HTTP handler and the
// MCP tool. One implementation, deliberately: an agent and a browser asking the
// same question must get the same answer, and two code paths would drift on the
// first change to the prompt or the grounding check.
func (h *Handler) Answer(ctx context.Context, req AskRequest) (Answer, error) {
	hits, err := h.store.Search(ctx, req.Query, maxSourcesInPrompt)
	if err != nil {
		return Answer{}, err
	}

	if len(hits) == 0 {
		// Answered without calling the model at all. Faster, cheaper, and
		// truthful — there is genuinely nothing here to answer from.
		return Answer{
			Query:     req.Query,
			Text:      "Bu soru bilgi tabanındaki belgelerde geçmiyor.",
			Sources:   []Source{},
			Grounded:  true,
			NoResults: true,
		}, nil
	}

	// Checked after retrieval, not before: with no inference host the search
	// results are still worth returning, and the caller is told the answer
	// specifically — not the whole feature — is unavailable.
	if h.gen == nil || !h.gen.Configured() {
		return Answer{}, common.ErrUnavailable(
			"Çıkarım sunucusu şu an kapalı. Arama çalışmaya devam ediyor.")
	}

	sources := SourcesFrom(hits)
	model := h.resolveModel(ctx, req.Model)

	completion, err := h.gen.Generate(ctx, llm.CompletionRequest{
		Model:        model,
		Prompt:       BuildPrompt(req.Query, sources),
		SystemPrompt: SystemPrompt,
		Temperature:  answerTemperature,
		MaxTokens:    answerMaxTokens,
	})
	if err != nil {
		return Answer{}, err
	}

	text, sources, grounded := Ground(completion.Content, sources)
	if completion.Truncated {
		// Said plainly rather than left for the reader to infer from a sentence
		// that stops mid-word. A cut-off answer is not a wrong one, but it is
		// not a complete one either.
		text += "\n\n(Yanıt token sınırında kesildi.)"
	}

	return Answer{
		Query:     req.Query,
		Text:      text,
		Sources:   OrderSourcesByCitation(sources),
		Grounded:  grounded,
		Model:     model,
		LatencyMs: completion.LatencyMs,
	}, nil
}

// resolveModel follows the same precedence as the analysis path: an explicit
// request wins, then the operator's default, then the active adapter, then the
// compiled-in fallback. The explicit choice sits above the active adapter so
// the untuned base can be served deliberately during an evaluation.
func (h *Handler) resolveModel(ctx context.Context, requested string) string {
	if requested != "" {
		return requested
	}
	cfg, err := h.set.Get(ctx)
	if err != nil {
		return defaultModel
	}
	switch {
	case cfg.DefaultModel != "":
		return cfg.DefaultModel
	case cfg.ActiveModelID != "":
		return cfg.ActiveModelID
	default:
		return defaultModel
	}
}

// List returns the catalogue. GET /wiki/documents
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	docs, err := h.store.List(r.Context())
	if err != nil {
		common.Error(w, common.ErrInternal("could not list documents"))
		return
	}
	common.JSON(w, http.StatusOK, map[string]any{"documents": docs, "count": len(docs)})
}

// Get returns one document with its body. GET /wiki/documents/{slug}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	d, err := h.store.Get(r.Context(), chi.URLParam(r, "slug"))
	if errors.Is(err, ErrNoRows) {
		common.Error(w, common.ErrNotFound("document not found"))
		return
	}
	if err != nil {
		common.Error(w, common.ErrInternal("could not read document"))
		return
	}
	common.JSON(w, http.StatusOK, d)
}

// ---- write paths (admin) ----

// IngestRequest adds or replaces a document.
type IngestRequest struct {
	Slug      string   `json:"slug"`
	Title     string   `json:"title"`
	SourceURL string   `json:"source_url"`
	Body      string   `json:"body"`
	Tags      []string `json:"tags"`
}

// slugRE bounds what may become part of a URL and a citation. Deliberately
// narrow: a slug appears in links and in the text of answers, and permitting
// arbitrary characters there invites both encoding bugs and injection into
// anything that renders a citation.
var slugRE = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// Ingest stores a document. POST /wiki/documents
func (h *Handler) Ingest(w http.ResponseWriter, r *http.Request) {
	claims, _ := common.ClaimsFromContext(r.Context())

	var req IngestRequest
	if err := common.Decode(r, &req); err != nil {
		common.Error(w, common.ErrBadRequest("invalid JSON body"))
		return
	}

	req.Slug = strings.ToLower(strings.TrimSpace(req.Slug))
	req.Title = strings.TrimSpace(req.Title)
	req.Body = strings.TrimSpace(req.Body)

	switch {
	case !slugRE.MatchString(req.Slug):
		common.Error(w, common.ErrBadRequest(
			"slug must be lowercase letters, digits and single hyphens, e.g. yatirim-kriterleri"))
		return
	case req.Title == "" || len([]rune(req.Title)) > maxTitleChars:
		common.Error(w, common.ErrBadRequest("title is required and must be under 200 characters"))
		return
	case req.Body == "":
		common.Error(w, common.ErrBadRequest("body is required"))
		return
	case len(req.Body) > maxBodyChars:
		common.Error(w, common.ErrBadRequest("body is too large; split it into several documents"))
		return
	case len(req.Tags) > maxDocumentTags:
		common.Error(w, common.ErrBadRequest("too many tags"))
		return
	}
	if req.Tags == nil {
		req.Tags = []string{}
	}

	d, err := h.store.Ingest(r.Context(), claims.UserID, Document{
		Slug:      req.Slug,
		Title:     req.Title,
		SourceURL: req.SourceURL,
		Body:      req.Body,
		Tags:      req.Tags,
	})
	if err != nil {
		slog.Error("wiki ingest failed", "slug", req.Slug, "error", err)
		common.Error(w, common.ErrInternal("could not ingest document"))
		return
	}

	slog.Info("wiki document ingested", "slug", d.Slug, "chunks", d.Chunks, "user_id", claims.UserID)
	common.JSON(w, http.StatusCreated, d)
}

// Delete removes a document and its chunks. DELETE /wiki/documents/{slug}
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if err := h.store.Delete(r.Context(), slug); err != nil {
		if errors.Is(err, ErrNoRows) {
			common.Error(w, common.ErrNotFound("document not found"))
			return
		}
		common.Error(w, common.ErrInternal("could not delete document"))
		return
	}
	slog.Info("wiki document deleted", "slug", slug)
	w.WriteHeader(http.StatusNoContent)
}

// Routes mounts DeepKwiki.
//
// Reads are open to any signed-in user; writes are admin-only. The split is
// deliberate rather than incidental: the knowledge base is what every answer is
// grounded in, so whoever can write to it can change what the product asserts
// to everyone. That is a control-plane power, not a user one.
//
// Two timeouts, because asking waits on the GPU across a tunnel and searching
// does not.
func (h *Handler) Routes(
	verify common.TokenVerifier,
	defaultTimeout, genTimeout time.Duration,
) http.Handler {
	r := chi.NewRouter()

	r.Group(func(pr chi.Router) {
		pr.Use(common.RequireAuth(verify))

		pr.Group(func(sr chi.Router) {
			sr.Use(common.Timeout(defaultTimeout))
			sr.Get("/search", h.Search)
			sr.Get("/documents", h.List)
			sr.Get("/documents/{slug}", h.Get)
		})

		pr.With(common.Timeout(genTimeout)).Post("/ask", h.Ask)

		pr.Group(func(ar chi.Router) {
			ar.Use(common.RequireRole(common.RoleAdmin))
			ar.Use(common.Timeout(defaultTimeout))
			ar.Post("/documents", h.Ingest)
			ar.Delete("/documents/{slug}", h.Delete)
		})
	})

	return r
}
