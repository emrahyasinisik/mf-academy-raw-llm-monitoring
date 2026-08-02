package decision

// Tavily authenticates with an Authorization header. This package sent the key
// in the request body instead — the form Tavily's API took years ago — so a
// valid, paid-for key would have come back 401 and the persona would have
// researched nothing, exactly as it did on the keyless fallback.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTavilyAuthenticatesWithABearerHeader(t *testing.T) {
	var gotAuth string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"results":[{"title":"T","url":"https://e.com","content":"c"}]}`)
	}))
	defer srv.Close()

	s := &tavilySearcher{apiKey: "tvly-test", client: srv.Client(), endpoint: srv.URL}

	res, err := s.Search(context.Background(), "acme ai", 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotAuth != "Bearer tvly-test" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer tvly-test")
	}
	if _, leaked := gotBody["api_key"]; leaked {
		t.Error("the key must not also travel in the body, where it lands in request logs")
	}
	if len(res) != 1 || res[0].Snippet != "c" {
		t.Errorf("the extracted content must survive parsing, got %+v", res)
	}
}

// A key that a provider rejects has to reach the operator as a failure, not as
// an empty result set — that confusion is the whole reason this file exists.
func TestTavilyReportsARejectedKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"detail":"invalid api key"}`)
	}))
	defer srv.Close()

	s := &tavilySearcher{apiKey: "wrong", client: srv.Client(), endpoint: srv.URL}

	_, err := s.Search(context.Background(), "acme ai", 3)
	if err == nil {
		t.Fatal("a 401 must be an error, not zero results")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("the error should name the status, got %q", err)
	}
}

// NewSearcher's contract, since the deployment depends on it: the keyless
// fallback is what runs when the key is missing, whatever the provider says.
func TestNewSearcherFallsBackWithoutAKey(t *testing.T) {
	if got := NewSearcher("tavily", "", time.Second).Name(); got != "duckduckgo" {
		t.Errorf("provider without a key = %q, want the keyless fallback", got)
	}
	if got := NewSearcher("tavily", "tvly-x", time.Second).Name(); got != "tavily" {
		t.Errorf("provider with a key = %q, want tavily", got)
	}
}
