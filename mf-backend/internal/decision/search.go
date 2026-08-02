package decision

// Live web research for the investment persona. The agent decides *that* it
// needs to research; this file decides *how*, behind a Searcher interface so the
// provider can change without the agent noticing.
//
// Two providers ship. Tavily returns already-extracted page content and is the
// one to use in production; it needs an API key. DuckDuckGo needs no account and
// is the default so the agent runs on a fresh deployment — but it scrapes an
// HTML page and returns only snippets, so treat its evidence as thinner.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// SearchResult is one live web finding the agent may cite. Snippet is whatever
// text the provider could give for the page — extracted content from Tavily, a
// result summary from DuckDuckGo — and is what the model reasons over.
type SearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

// Searcher runs a live web query. The agent depends on this interface, never on
// a concrete provider, so swapping search is a wiring change, not a code change.
type Searcher interface {
	Search(ctx context.Context, query string, limit int) ([]SearchResult, error)
	// Name identifies the provider in logs and in the evidence shown to the
	// user, so a thin answer can be traced to a keyless fallback.
	Name() string
}

const defaultSearchLimit = 5

// NewSearcher picks a provider from configuration: Tavily when it is both
// requested and keyed, the keyless DuckDuckGo scrape otherwise.
func NewSearcher(provider, apiKey string, timeout time.Duration) Searcher {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	client := &http.Client{Timeout: timeout}
	if strings.EqualFold(strings.TrimSpace(provider), "tavily") && strings.TrimSpace(apiKey) != "" {
		return &tavilySearcher{apiKey: apiKey, client: client}
	}
	return &ddgSearcher{client: client}
}

// ---- Tavily ----

type tavilySearcher struct {
	apiKey string
	client *http.Client
	// endpoint is the API's address, injected only so the tests can point it at
	// a local server. Empty means the real one.
	endpoint string
}

const tavilyEndpoint = "https://api.tavily.com/search"

func (t *tavilySearcher) Name() string { return "tavily" }

func (t *tavilySearcher) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	// search_depth basic is one credit per call; advanced is two and returns
	// more of each page than a 1366-token evidence budget can carry anyway.
	body, err := json.Marshal(map[string]any{
		"query":        query,
		"max_results":  limit,
		"search_depth": "basic",
	})
	if err != nil {
		return nil, err
	}
	endpoint := t.endpoint
	if endpoint == "" {
		endpoint = tavilyEndpoint
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	// The key goes in the header, not the body. Tavily accepted an `api_key`
	// body field in an earlier version of this API and this package was written
	// against that one; a current key sent that way comes back 401, which — now
	// that a failed tool is reported as failed — surfaces as "web · çalışmadı"
	// rather than as a subject nobody has written about.
	req.Header.Set("Authorization", "Bearer "+t.apiKey)

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("tavily returned %d", resp.StatusCode)
	}

	var parsed struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	out := make([]SearchResult, 0, len(parsed.Results))
	for _, r := range parsed.Results {
		out = append(out, SearchResult{
			Title:   strings.TrimSpace(r.Title),
			URL:     strings.TrimSpace(r.URL),
			Snippet: strings.TrimSpace(r.Content),
		})
	}
	return out, nil
}

// ---- DuckDuckGo (keyless HTML scrape) ----

type ddgSearcher struct {
	client *http.Client
}

func (d *ddgSearcher) Name() string { return "duckduckgo" }

// The HTML endpoint returns a static results page — no JavaScript needed — with
// each result as an <a class="result__a"> title link and a matching
// <a class="result__snippet">. The href is a DuckDuckGo redirect carrying the
// real URL in its uddg query parameter, so it is unwrapped rather than followed.
var (
	ddgLinkRE    = regexp.MustCompile(`(?s)<a[^>]+class="result__a"[^>]+href="([^"]+)"[^>]*>(.*?)</a>`)
	ddgSnippetRE = regexp.MustCompile(`(?s)<a[^>]+class="result__snippet"[^>]*>(.*?)</a>`)
	tagRE        = regexp.MustCompile(`<[^>]+>`)
)

func (d *ddgSearcher) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	endpoint := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(query)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	// DuckDuckGo serves an empty page to clients with no User-Agent.
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; mf-backend/0.1; +https://masterfabric)")

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("duckduckgo returned %d", resp.StatusCode)
	}

	page := string(raw)
	links := ddgLinkRE.FindAllStringSubmatch(page, -1)
	snippets := ddgSnippetRE.FindAllStringSubmatch(page, -1)

	out := make([]SearchResult, 0, limit)
	for i, m := range links {
		if len(out) >= limit {
			break
		}
		href := unwrapDDG(m[1])
		if href == "" {
			continue
		}
		snippet := ""
		if i < len(snippets) {
			snippet = cleanHTML(snippets[i][1])
		}
		out = append(out, SearchResult{
			Title:   cleanHTML(m[2]),
			URL:     href,
			Snippet: snippet,
		})
	}
	return out, nil
}

// unwrapDDG turns a //duckduckgo.com/l/?uddg=<encoded>&… redirect into the real
// destination URL. A link that is already absolute is returned as-is.
func unwrapDDG(href string) string {
	href = html.UnescapeString(href)
	if strings.HasPrefix(href, "//") {
		href = "https:" + href
	}
	u, err := url.Parse(href)
	if err != nil {
		return ""
	}
	if uddg := u.Query().Get("uddg"); uddg != "" {
		return uddg
	}
	if strings.HasPrefix(href, "http") {
		return href
	}
	return ""
}

// cleanHTML strips tags and unescapes entities so a snippet is plain text the
// model can read and the reader can trust as the page's own words.
func cleanHTML(s string) string {
	s = tagRE.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	return strings.TrimSpace(s)
}
