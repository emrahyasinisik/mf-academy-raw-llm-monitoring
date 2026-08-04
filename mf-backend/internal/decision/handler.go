package decision

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/emrah/mf-backend/internal/common"
)

// Bounds on one conversation. The persona re-reads the whole history each turn
// and re-researches from it, so an unbounded transcript is both an unbounded
// prompt and an unbounded bill against a 6 GB card.
const (
	maxTurns      = 40
	maxTurnChars  = 8000
	minLatestRune = 2
)

// Defaults and bounds for the history list.
const (
	defaultListLimit = 20
	maxListLimit     = 100
)

// writeTimeout bounds the history write that follows a completed turn. It runs
// on a context detached from the request, so without a bound of its own a stuck
// database would hold the goroutine after the client has long since gone.
const writeTimeout = 5 * time.Second

// conversationStore is the persistence the HTTP handlers need. Declared
// consumer-side so Patch can be httptest'd without PostgreSQL.
type conversationStore interface {
	Record(ctx context.Context, userID, conversationID, latest string, res Result) (string, error)
	List(ctx context.Context, userID string, limit int, before time.Time) (ListResult, error)
	Get(ctx context.Context, userID, id string) (Conversation, error)
	Rename(ctx context.Context, userID, id, title string) error
	SetAssessmentID(ctx context.Context, userID, id string, assessmentID *string) error
	Delete(ctx context.Context, userID, id string) error
}

// Handler is the HTTP surface for the investment persona.
type Handler struct {
	agent       *Agent
	store       conversationStore
	assessments AssessmentOwner
}

func NewHandler(agent *Agent, store *Store, assessments AssessmentOwner) *Handler {
	return &Handler{agent: agent, store: store, assessments: assessments}
}

// ChatRequest is the whole conversation, newest message last.
type ChatRequest struct {
	Messages []Turn `json:"messages"`
	// ConversationID names the thread this turn belongs to. Empty opens a new
	// one. Optional rather than required so a client that has not been updated
	// keeps working exactly as before — it just gets an id back it ignores.
	ConversationID string `json:"conversation_id"`
}

// ChatResponse is the agent's result plus the thread it was recorded in.
//
// Result is embedded rather than nested so the reply keeps the shape the browser
// already reads; conversation_id is additive.
type ChatResponse struct {
	Result
	// ConversationID is empty when the turn could not be recorded. That is a
	// deliberate signal and not an omission: the answer is real, the history is
	// not, and a client that stored this id would later resume a thread the
	// server does not have.
	ConversationID string `json:"conversation_id"`
}

// Chat runs one turn of the persona. POST /decision/chat
//
// The client owns the transcript and sends it entire each turn; the agent is
// stateless between turns and reads the history out of the request, not the
// database. That keeps it horizontally scalable — two instances behind a load
// balancer answer identically — and it is why the history write below is a side
// effect of a turn rather than a step in producing one.
//
// The consequence is deliberate: if the write fails, the caller still gets the
// answer. A turn costs a live web search and a generation on a single 6 GB card,
// and throwing that away because a Postgres connection blipped would trade a
// real answer for a clean audit trail. The failure is logged and reported by an
// empty conversation_id rather than by a 500.
func (h *Handler) Chat(w http.ResponseWriter, r *http.Request) {
	claims, _ := common.ClaimsFromContext(r.Context())

	var req ChatRequest
	if err := common.Decode(r, &req); err != nil {
		common.Error(w, common.ErrBadRequest("invalid JSON body"))
		return
	}

	turns, err := sanitize(req.Messages)
	if err != nil {
		common.Error(w, err)
		return
	}

	res, err := h.agent.Respond(r.Context(), turns)
	if err != nil {
		// The provider returns *APIError for expected states — the inference
		// host being asleep is not a bug and must reach the user as itself.
		var apiErr *common.APIError
		if errors.As(err, &apiErr) {
			common.Error(w, apiErr)
			return
		}
		slog.Error("decision chat failed", "error", err)
		common.Error(w, common.ErrInternal("could not produce a response"))
		return
	}

	out := ChatResponse{Result: res}
	latest := turns[len(turns)-1].Content

	// A fresh context for the write. The request's own may already be at its
	// deadline — generation is allowed to consume nearly all of it — and a
	// history write cancelled a millisecond after the model finished would lose
	// the most expensive turn in the system to a clock rather than to a fault.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), writeTimeout)
	defer cancel()

	id, err := h.store.Record(ctx, claims.UserID, req.ConversationID, latest, res)
	switch {
	case err == nil:
		out.ConversationID = id
	case errors.Is(err, ErrNoRows):
		// The named thread is gone or was never theirs. The answer stands; the
		// client is told, by the empty id, to treat this as unthreaded rather
		// than to keep appending to an id the server rejects.
		slog.Warn("decision history: unknown conversation",
			"user_id", claims.UserID, "conversation_id", req.ConversationID)
	default:
		slog.Error("decision history write failed", "error", err, "user_id", claims.UserID)
	}

	common.JSON(w, http.StatusOK, out)
}

// List returns a page of the user's threads. GET /decision/conversations
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	claims, _ := common.ClaimsFromContext(r.Context())

	limit := defaultListLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxListLimit {
			common.Error(w, common.ErrBadRequest(
				"limit must be between 1 and "+strconv.Itoa(maxListLimit)))
			return
		}
		limit = n
	}

	var before time.Time
	if v := r.URL.Query().Get("before"); v != "" {
		t, err := time.Parse(time.RFC3339Nano, v)
		if err != nil {
			common.Error(w, common.ErrBadRequest("before must be an RFC3339 timestamp"))
			return
		}
		before = t
	}

	res, err := h.store.List(r.Context(), claims.UserID, limit, before)
	if err != nil {
		slog.Error("decision history list failed", "error", err)
		common.Error(w, common.ErrInternal("could not list conversations"))
		return
	}
	common.JSON(w, http.StatusOK, res)
}

// Get returns one thread with its transcript. GET /decision/conversations/{id}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	claims, _ := common.ClaimsFromContext(r.Context())

	c, err := h.store.Get(r.Context(), claims.UserID, chi.URLParam(r, "id"))
	if errors.Is(err, ErrNoRows) {
		common.Error(w, common.ErrNotFound("no such conversation"))
		return
	}
	if err != nil {
		slog.Error("decision history read failed", "error", err)
		common.Error(w, common.ErrInternal("could not load the conversation"))
		return
	}
	common.JSON(w, http.StatusOK, c)
}

// Delete removes a thread and its messages. DELETE /decision/conversations/{id}
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	claims, _ := common.ClaimsFromContext(r.Context())

	err := h.store.Delete(r.Context(), claims.UserID, chi.URLParam(r, "id"))
	if errors.Is(err, ErrNoRows) {
		common.Error(w, common.ErrNotFound("no such conversation"))
		return
	}
	if err != nil {
		slog.Error("decision history delete failed", "error", err)
		common.Error(w, common.ErrInternal("could not delete the conversation"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// sanitize enforces the conversation shape the agent assumes: a bounded number
// of non-empty turns, ending in a user message long enough to act on.
func sanitize(in []Turn) ([]Turn, error) {
	out := make([]Turn, 0, len(in))
	for _, t := range in {
		content := strings.TrimSpace(t.Content)
		if content == "" {
			continue
		}
		if len(content) > maxTurnChars {
			return nil, common.ErrBadRequest("a message is too long")
		}
		role := "user"
		if t.Role == "assistant" {
			role = "assistant"
		}
		out = append(out, Turn{Role: role, Content: content})
	}
	if len(out) == 0 {
		return nil, common.ErrBadRequest("no messages")
	}
	if len(out) > maxTurns {
		return nil, common.ErrBadRequest("conversation is too long")
	}
	last := out[len(out)-1]
	if last.Role != "user" {
		return nil, common.ErrBadRequest("the last message must be from the user")
	}
	if len([]rune(last.Content)) < minLatestRune {
		return nil, common.ErrBadRequest("message is too short")
	}
	return out, nil
}

// Prompt exposes the exact persona prompt the agent sends the model. GET
// /decision/prompt.
//
// It exists for the fine-tune, not the UI. An adapter learns to obey one
// specific system prompt and turn instruction; if the training set reproduced
// them by hand it would drift the first time either is edited here, and the
// adapter would be tuned for a prompt nothing sends — a failure that is
// invisible, because training completes and the loss looks fine. So the dataset
// builder reads these strings from the running backend instead.
func (h *Handler) Prompt(w http.ResponseWriter, r *http.Request) {
	common.JSON(w, http.StatusOK, map[string]any{
		"system_prompt":    personaSystemPrompt,
		"turn_instruction": turnInstruction,
		// The header the evidence block opens with. The rest of that block is
		// assembled per source in agent.gather; the builder mirrors that layout
		// and this pins the one line it cannot infer.
		"evidence_header": evidenceHeader,
	})
}

// Routes mounts the persona under /decision. /chat is one slow route — it
// researches live and then waits on the GPU — so it takes the generation
// timeout; everything else is a database read or a static one and takes the
// short bound.
func (h *Handler) Routes(verify common.TokenVerifier, defaultTimeout, genTimeout time.Duration) http.Handler {
	r := chi.NewRouter()
	r.Group(func(pr chi.Router) {
		pr.Use(common.RequireAuth(verify))
		pr.Use(common.RequirePasswordFresh)
		pr.With(common.Timeout(genTimeout)).Post("/chat", h.Chat)

		pr.Group(func(sr chi.Router) {
			sr.Use(common.Timeout(defaultTimeout))
			sr.Get("/prompt", h.Prompt)
			sr.Get("/conversations", h.List)
			sr.Get("/conversations/{id}", h.Get)
			sr.Patch("/conversations/{id}", h.Patch)
			sr.Delete("/conversations/{id}", h.Delete)
		})
	})
	return r
}
