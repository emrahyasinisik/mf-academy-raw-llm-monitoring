package decision

import (
	"errors"
	"log/slog"
	"net/http"
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

// Handler is the HTTP surface for the investment persona.
type Handler struct {
	agent *Agent
}

func NewHandler(agent *Agent) *Handler {
	return &Handler{agent: agent}
}

// ChatRequest is the whole conversation, newest message last.
type ChatRequest struct {
	Messages []Turn `json:"messages"`
}

// Chat runs one turn of the persona. POST /decision/chat
//
// The client owns the transcript and sends it entire each turn; the server is
// stateless between turns. That keeps the agent horizontally scalable and means
// a reload never loses a conversation the client still holds.
func (h *Handler) Chat(w http.ResponseWriter, r *http.Request) {
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
	common.JSON(w, http.StatusOK, res)
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
// timeout; /prompt is a static read and takes the short one.
func (h *Handler) Routes(verify common.TokenVerifier, defaultTimeout, genTimeout time.Duration) http.Handler {
	r := chi.NewRouter()
	r.Group(func(pr chi.Router) {
		pr.Use(common.RequireAuth(verify))
		pr.With(common.Timeout(genTimeout)).Post("/chat", h.Chat)
		pr.With(common.Timeout(defaultTimeout)).Get("/prompt", h.Prompt)
	})
	return r
}
