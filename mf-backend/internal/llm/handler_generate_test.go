package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/emrah/mf-backend/internal/common"
)

// ---- doubles ----

// fakeStore records what the handler asked to persist. Only CreateRun and
// UpsertScore are exercised by the generation path; the rest satisfy RunStore.
type fakeStore struct {
	created CreateRunRequest
	err     error
}

func (f *fakeStore) CreateRun(_ context.Context, userID string, req CreateRunRequest) (Run, error) {
	f.created = req
	if f.err != nil {
		return Run{}, f.err
	}
	return Run{
		ID: "run-1", UserID: userID, Model: req.Model, Target: req.Target,
		Prompt: req.Prompt, Response: req.Response,
		PromptTokens: req.PromptTokens, CompletionTokens: req.CompletionTokens,
		LatencyMs: req.LatencyMs, CreatedAt: time.Now(),
	}, nil
}

func (f *fakeStore) GetRun(context.Context, string, string) (Run, error) { return Run{}, nil }
func (f *fakeStore) ListRuns(context.Context, string, string, int, time.Time) (ListResult, error) {
	return ListResult{}, nil
}
func (f *fakeStore) DeleteRun(context.Context, string, string) error { return nil }
func (f *fakeStore) UpsertScore(_ context.Context, sc Score) (Score, error) {
	sc.ID = "score-1"
	return sc, nil
}
func (f *fakeStore) Metrics(context.Context, string) (Metrics, error) { return Metrics{}, nil }

// fakeGen stands in for the GPU host, which is a desktop machine that is
// usually off. `configured` false models LLM_BASE_URL being unset.
type fakeGen struct {
	configured bool
	result     Completion
	err        error
	gotReq     CompletionRequest
}

func (g *fakeGen) Configured() bool { return g.configured }
func (g *fakeGen) Generate(_ context.Context, req CompletionRequest) (Completion, error) {
	g.gotReq = req
	return g.result, g.err
}

func postGenerate(h *Handler, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/llm/generate", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r = r.WithContext(common.ContextWithClaims(r.Context(), common.AuthClaims{UserID: "user-1"}))
	w := httptest.NewRecorder()
	h.GenerateRun(w, r)
	return w
}

// ---- tests ----

func TestGenerateRunTagsTheRunAsServerExecuted(t *testing.T) {
	store := &fakeStore{}
	gen := &fakeGen{
		configured: true,
		result: Completion{
			Content: "A goroutine is a lightweight thread.", PromptTokens: 11,
			CompletionTokens: 7, LatencyMs: 670,
		},
	}
	h := NewHandler(store, gen)

	w := postGenerate(h, `{
		"model": "gemma-2-2b-it-q4f16_1-MLC",
		"prompt": "What is a goroutine?",
		"expected_keywords": ["goroutine"],
		"auto_score": true
	}`)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	// The whole point of the column: a server run must never be recorded as a
	// browser one, or the latency comparison is silently wrong.
	if store.created.Target != TargetServer {
		t.Errorf("target = %q, want %q", store.created.Target, TargetServer)
	}
	// Timings come from the provider, not from anything the client sent.
	if store.created.LatencyMs != 670 || store.created.CompletionTokens != 7 {
		t.Errorf("telemetry = %dms/%d tokens, want 670/7",
			store.created.LatencyMs, store.created.CompletionTokens)
	}

	var run Run
	if err := json.Unmarshal(w.Body.Bytes(), &run); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if run.Score == nil {
		t.Error("auto_score was true but no score came back")
	}
}

func TestGenerateRunIsUnavailableWithoutAnInferenceHost(t *testing.T) {
	// LLM_BASE_URL unset. This is a normal deployment, not a broken one: the
	// browser path still works, so this must not be a 500.
	h := NewHandler(&fakeStore{}, &fakeGen{configured: false})

	w := postGenerate(h, `{"model":"gemma-2-2b-it-q4f16_1-MLC","prompt":"hi"}`)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}

func TestGenerateRunRejectsAModelTheServerCannotRun(t *testing.T) {
	gen := &fakeGen{configured: true}
	h := NewHandler(&fakeStore{}, gen)

	// Browser-only in the catalogue. Caught here rather than surfacing later as
	// an opaque upstream error.
	w := postGenerate(h, `{"model":"Phi-3.5-mini-instruct-q4f16_1-MLC","prompt":"hi"}`)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	if gen.gotReq.Model != "" {
		t.Error("unsupported model still reached the inference host")
	}
}

func TestGenerateRunRequiresAPrompt(t *testing.T) {
	h := NewHandler(&fakeStore{}, &fakeGen{configured: true})

	// Whitespace is not a prompt; sending it would spend GPU time on nothing.
	w := postGenerate(h, `{"model":"gemma-2-2b-it-q4f16_1-MLC","prompt":"   "}`)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestGenerateRunPropagatesUpstreamStatus(t *testing.T) {
	gen := &fakeGen{
		configured: true,
		err:        common.ErrUpstreamTimeout("inference host did not answer in time"),
	}
	h := NewHandler(&fakeStore{}, gen)

	w := postGenerate(h, `{"model":"gemma-2-2b-it-q4f16_1-MLC","prompt":"hi"}`)

	// A slow GPU is not an internal server error, and the distinction is what
	// tells the operator whether to wake the machine or wait.
	if w.Code != http.StatusGatewayTimeout {
		t.Errorf("status = %d, want %d", w.Code, http.StatusGatewayTimeout)
	}
}

func TestModelsReportsWhetherServerInferenceIsAvailable(t *testing.T) {
	for _, configured := range []bool{true, false} {
		h := NewHandler(&fakeStore{}, &fakeGen{configured: configured})
		r := httptest.NewRequest(http.MethodGet, "/llm/models", nil)
		w := httptest.NewRecorder()
		h.Models(w, r)

		var body struct {
			ServerInference bool        `json:"server_inference"`
			Models          []ModelInfo `json:"models"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		// The frontend hides the server option on this flag; getting it wrong
		// offers users a button that can only fail.
		if body.ServerInference != configured {
			t.Errorf("server_inference = %v, want %v", body.ServerInference, configured)
		}
		if len(body.Models) == 0 || len(body.Models[0].Targets) == 0 {
			t.Error("catalogue must state where each model can run")
		}
	}
}
