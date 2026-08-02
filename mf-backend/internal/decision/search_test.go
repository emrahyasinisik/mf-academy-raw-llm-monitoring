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

// ---- DuckDuckGo ----
//
// The keyless scrape is what runs in production, so the page it cannot read has
// to be told apart from the page that says there is nothing to read. Both are
// 200s, and treating the first as the second is how "this market has no
// coverage" gets printed under a verdict.

const ddgResultPage = `<html><body>
<div class="result"><a class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Facme.example%2Fabout">Acme AI</a>
<a class="result__snippet">B2B SaaS, Seri A öncesi.</a></div>
<div class="result"><a class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fpress.example%2Fnews">Acme raised</a>
<a class="result__snippet">Yatırım turu haberi.</a></div>
</body></html>`

// What DuckDuckGo actually serves for a query nothing matches — measured, not
// invented: the marker below is the class it wraps that message in.
const ddgNoResultsPage = `<html><body>
<div class="result results_links web-result result--no-result">
<div class="no-results__container result__title">No results</div></div>
</body></html>`

// A challenge or rate-limit interstitial: 200, no results, and no statement
// that there are none.
const ddgBlockedPage = `<html><body><div class="anomaly-modal__title">
If this error persists, please let us know.</div></body></html>`

func ddgServing(t *testing.T, page string) (*ddgSearcher, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, page)
	}))
	return &ddgSearcher{client: srv.Client(), endpoint: srv.URL}, srv.Close
}

func TestDuckDuckGoReadsTheResultPage(t *testing.T) {
	d, done := ddgServing(t, ddgResultPage)
	defer done()

	res, err := d.Search(context.Background(), "acme ai", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("got %d results, want 2", len(res))
	}
	if res[0].URL != "https://acme.example/about" {
		t.Errorf("the redirect must be unwrapped, got %q", res[0].URL)
	}
	if res[0].Snippet != "B2B SaaS, Seri A öncesi." {
		t.Errorf("snippet = %q", res[0].Snippet)
	}
}

func TestDuckDuckGoAcceptsAPageThatSaysThereAreNoResults(t *testing.T) {
	d, done := ddgServing(t, ddgNoResultsPage)
	defer done()

	res, err := d.Search(context.Background(), "zxqwv ttrpq", 5)
	if err != nil {
		t.Fatalf("a page that states it has no results is an answer, not a failure: %v", err)
	}
	if len(res) != 0 {
		t.Errorf("got %d results, want none", len(res))
	}
}

func TestDuckDuckGoReportsAPageItCouldNotRead(t *testing.T) {
	d, done := ddgServing(t, ddgBlockedPage)
	defer done()

	_, err := d.Search(context.Background(), "acme ai", 5)
	if err == nil {
		t.Fatal("a 200 with neither results nor a no-results notice must be an error — it is a block or a layout change, not an empty market")
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
