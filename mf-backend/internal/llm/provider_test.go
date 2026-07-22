package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/emrah/mf-backend/internal/common"
)

// The real inference host is a GPU desktop that is often switched off, so these
// tests stand in for it with an httptest server speaking the same dialect. They
// cover the behaviour that is expensive to discover in production: which HTTP
// failure maps to which status, and whether the shared secret is actually sent.

func TestGenerateParsesAnOpenAICompatibleResponse(t *testing.T) {
	var gotKey, gotBearer, gotPath string
	var gotBody chatRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-API-Key")
		gotBearer = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"choices": [{"message": {"role": "assistant", "content": "A goroutine is a lightweight thread."}}],
			"usage": {"prompt_tokens": 11, "completion_tokens": 7}
		}`))
	}))
	defer srv.Close()

	p := NewOpenAIProvider(srv.URL, "s3cret", 5*time.Second, 0)
	got, err := p.Generate(context.Background(), CompletionRequest{
		Model:        "gemma-2-2b-it-q4f16_1-MLC",
		Prompt:       "What is a goroutine?",
		SystemPrompt: "Be brief.",
		Temperature:  0.7,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if got.Content != "A goroutine is a lightweight thread." {
		t.Errorf("content = %q", got.Content)
	}
	if got.PromptTokens != 11 || got.CompletionTokens != 7 {
		t.Errorf("usage = %d/%d, want 11/7", got.PromptTokens, got.CompletionTokens)
	}
	if got.LatencyMs < 0 {
		t.Errorf("latency = %d, want >= 0", got.LatencyMs)
	}

	if gotPath != "/v1/chat/completions" {
		t.Errorf("path = %q", gotPath)
	}
	// Both headers matter: the Caddy gateway reads X-API-Key, a hosted provider
	// reads the bearer token. Dropping either silently breaks one deployment.
	if gotKey != "s3cret" {
		t.Errorf("X-API-Key = %q", gotKey)
	}
	if gotBearer != "Bearer s3cret" {
		t.Errorf("Authorization = %q", gotBearer)
	}

	// A system prompt must precede the user turn, and generation must be bounded.
	if len(gotBody.Messages) != 2 || gotBody.Messages[0].Role != "system" || gotBody.Messages[1].Role != "user" {
		t.Errorf("messages = %+v", gotBody.Messages)
	}
	if gotBody.MaxTokens != defaultMaxTokens {
		t.Errorf("max_tokens = %d, want %d", gotBody.MaxTokens, defaultMaxTokens)
	}
	if gotBody.Stream {
		t.Error("stream = true, want false")
	}
}

func TestGenerateOmitsAnEmptySystemPrompt(t *testing.T) {
	var gotBody chatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{}}`))
	}))
	defer srv.Close()

	p := NewOpenAIProvider(srv.URL, "", 5*time.Second, 0)
	if _, err := p.Generate(context.Background(), CompletionRequest{
		Model:        "m",
		Prompt:       "hi",
		SystemPrompt: "   ",
	}); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(gotBody.Messages) != 1 {
		t.Fatalf("messages = %+v, want just the user turn", gotBody.Messages)
	}
}

func TestGenerateCapsMaxTokensAtTheConfiguredCeiling(t *testing.T) {
	var gotBody chatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{}}`))
	}))
	defer srv.Close()

	p := NewOpenAIProvider(srv.URL, "", 5*time.Second, 128)
	// A caller asking for more than the ceiling must not get it: the ceiling is
	// what keeps one request from occupying the GPU until the deadline.
	if _, err := p.Generate(context.Background(), CompletionRequest{
		Model: "m", Prompt: "hi", MaxTokens: 100000,
	}); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if gotBody.MaxTokens != 128 {
		t.Errorf("max_tokens = %d, want 128", gotBody.MaxTokens)
	}
}

func TestGenerateMapsUpstreamFailures(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		wantStatus int
	}{
		{"rejected credentials", http.StatusUnauthorized, http.StatusBadGateway},
		{"upstream error", http.StatusInternalServerError, http.StatusBadGateway},
		{"model not found", http.StatusNotFound, http.StatusBadGateway},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"error":"internal detail that must not reach the client"}`))
			}))
			defer srv.Close()

			p := NewOpenAIProvider(srv.URL, "k", 5*time.Second, 0)
			_, err := p.Generate(context.Background(), CompletionRequest{Model: "m", Prompt: "hi"})

			var apiErr *common.APIError
			if !asAPIError(err, &apiErr) {
				t.Fatalf("error = %v, want *common.APIError", err)
			}
			if apiErr.Status != tc.wantStatus {
				t.Errorf("status = %d, want %d", apiErr.Status, tc.wantStatus)
			}
			// The upstream body is for our logs, not for the caller.
			if strings.Contains(apiErr.Message, "internal detail") {
				t.Errorf("upstream body leaked into client message: %q", apiErr.Message)
			}
		})
	}
}

func TestGenerateReportsAnUnreachableHostAsUnavailable(t *testing.T) {
	// A server that is closed immediately gives us a genuinely dead address —
	// the state the desktop inference host is in whenever it is asleep.
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	addr := srv.URL
	srv.Close()

	p := NewOpenAIProvider(addr, "k", 2*time.Second, 0)
	_, err := p.Generate(context.Background(), CompletionRequest{Model: "m", Prompt: "hi"})

	var apiErr *common.APIError
	if !asAPIError(err, &apiErr) {
		t.Fatalf("error = %v, want *common.APIError", err)
	}
	if apiErr.Status != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d (asleep is not a server bug)", apiErr.Status, http.StatusServiceUnavailable)
	}
}

func TestGenerateReportsASlowHostAsATimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	defer func() { close(release); srv.Close() }()

	p := NewOpenAIProvider(srv.URL, "k", 0, 0)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := p.Generate(ctx, CompletionRequest{Model: "m", Prompt: "hi"})

	var apiErr *common.APIError
	if !asAPIError(err, &apiErr) {
		t.Fatalf("error = %v, want *common.APIError", err)
	}
	// Distinct from unavailable: the host answered the connection, it just did
	// not finish generating. Waiting longer might have worked.
	if apiErr.Status != http.StatusGatewayTimeout {
		t.Errorf("status = %d, want %d", apiErr.Status, http.StatusGatewayTimeout)
	}
}

func TestGenerateWithoutAConfiguredHostIsUnavailable(t *testing.T) {
	p := NewOpenAIProvider("", "", time.Second, 0)
	if p.Configured() {
		t.Fatal("Configured() = true for an empty base URL")
	}
	_, err := p.Generate(context.Background(), CompletionRequest{Model: "m", Prompt: "hi"})

	var apiErr *common.APIError
	if !asAPIError(err, &apiErr) {
		t.Fatalf("error = %v, want *common.APIError", err)
	}
	if apiErr.Status != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", apiErr.Status, http.StatusServiceUnavailable)
	}
}

// asAPIError unwraps to *common.APIError, which is what the HTTP layer needs in
// order to answer with the right status.
func asAPIError(err error, target **common.APIError) bool {
	return errors.As(err, target)
}
