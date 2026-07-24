package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// The adapter control plane.
//
// This is the piece that makes activation take effect on a running server
// rather than on the next build. It talks to llama.cpp's /lora-adapters pair,
// which is a different shape from everything else in this package: not a
// generation, not OpenAI-dialect, and it changes global server state.
//
// What the runtime actually offers, stated precisely because the difference
// decides what the panel is allowed to promise:
//
//	adapters loaded at startup    fixed until the process restarts
//	the scale of a loaded adapter changeable at any time, takes effect at once
//
// So "hot-swap" here means switching between adapters the server already holds.
// Publishing a brand-new adapter still needs a restart of that one container —
// seconds, because the base is memory-mapped, but a restart. Activate() returns
// ErrAdapterNotLoaded for exactly that case so the caller can say so instead of
// reporting a success that did not happen.

// ErrAdapterNotLoaded means the file exists as far as the database is concerned
// but the running server was not started with it.
var ErrAdapterNotLoaded = errors.New("adapter is not loaded by the runtime")

// ErrRuntimeUnavailable means the hot-swap runtime could not be reached. Kept
// distinct from a failed swap: one is "the engine is down", the other is "the
// engine is up and refused", and an operator does different things about each.
var ErrRuntimeUnavailable = errors.New("hot-swap runtime unavailable")

// LoadedAdapter is one entry of the runtime's adapter table.
//
// Path is what the server was started with, so it is an absolute in-container
// path; the database stores bare file names. Matching happens on the base name,
// which is why Name exists alongside it.
type LoadedAdapter struct {
	ID    int     `json:"id"`
	Path  string  `json:"path"`
	Scale float64 `json:"scale"`
	// Name is Path's last segment — the form the database stores.
	Name string `json:"name"`
}

// AdapterRuntime is a client for the hot-swap runtime's control endpoints.
type AdapterRuntime struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

// NewAdapterRuntime builds a control client. baseURL points at the gateway
// prefix that routes to the hot-swap engine (…/rt), not at the engine directly:
// the engine publishes no host port, and going through the gateway means the
// control plane is behind the same shared secret as inference. An
// unauthenticated caller who could POST here would decide which adapter every
// user is served by.
func NewAdapterRuntime(baseURL, apiKey string, timeout time.Duration) *AdapterRuntime {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &AdapterRuntime{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		// A short timeout on purpose, unlike the generation client's. These
		// calls carry no tokens and return in milliseconds; if one is hanging,
		// the engine is wedged and waiting three minutes to be told so just
		// blocks the operator who is trying to find out.
		client: &http.Client{Timeout: timeout},
	}
}

// Configured reports whether a hot-swap runtime was wired at all. The product
// serves fine without one — the compiled path does not need it — so callers
// check this and degrade rather than the process refusing to start.
func (r *AdapterRuntime) Configured() bool { return r != nil && r.baseURL != "" }

func (r *AdapterRuntime) do(ctx context.Context, method, path string, body any) ([]byte, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encoding %s %s: %w", method, path, err)
		}
		rdr = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, r.baseURL+path, rdr)
	if err != nil {
		return nil, fmt.Errorf("building %s %s: %w", method, path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
	if r.apiKey != "" {
		// Both header forms, same reasoning as the generation client: the Caddy
		// gateway checks X-API-Key and anything hosted checks the bearer token.
		req.Header.Set("X-API-Key", r.apiKey)
		req.Header.Set("Authorization", "Bearer "+r.apiKey)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRuntimeUnavailable, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("%w: reading response: %v", ErrRuntimeUnavailable, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// 404 is worth naming: it is what the gateway answers when the runtime
		// is not in the compose stack at all, and it otherwise surfaces as an
		// opaque "unexpected status" that reads like a bug in this code.
		if resp.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("%w: %s %s returned 404 — is the llamacpp service running?",
				ErrRuntimeUnavailable, method, path)
		}
		return nil, fmt.Errorf("runtime returned %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}
	return raw, nil
}

// List returns the adapters the running server holds, with their current scale.
func (r *AdapterRuntime) List(ctx context.Context) ([]LoadedAdapter, error) {
	raw, err := r.do(ctx, http.MethodGet, "/lora-adapters", nil)
	if err != nil {
		return nil, err
	}
	var out []LoadedAdapter
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decoding adapter list: %w", err)
	}
	for i := range out {
		out[i].Name = baseName(out[i].Path)
	}
	return out, nil
}

// scaleEntry is one element of the POST body.
type scaleEntry struct {
	ID    int     `json:"id"`
	Scale float64 `json:"scale"`
}

// Activate makes exactly one adapter live and returns how long the swap took.
//
// The name is a bare file name, matching what the adapters table stores.
// Passing "" deactivates everything, which serves the untuned base.
//
// Every adapter's scale is sent on every call, including the ones being set to
// zero. The endpoint sets the scales it is given and leaves the rest alone, so
// sending only the winner would leave the previous adapter still applied and
// stack two fine-tunes on top of each other — output that is subtly wrong in a
// way no error surfaces.
func (r *AdapterRuntime) Activate(ctx context.Context, name string) (time.Duration, error) {
	if !r.Configured() {
		return 0, ErrRuntimeUnavailable
	}

	loaded, err := r.List(ctx)
	if err != nil {
		return 0, err
	}

	name = baseName(name)
	found := name == ""
	body := make([]scaleEntry, 0, len(loaded))
	for _, a := range loaded {
		scale := 0.0
		if name != "" && a.Name == name {
			scale = 1.0
			found = true
		}
		body = append(body, scaleEntry{ID: a.ID, Scale: scale})
	}

	if !found {
		have := make([]string, 0, len(loaded))
		for _, a := range loaded {
			have = append(have, a.Name)
		}
		return 0, fmt.Errorf("%w: %q; the runtime holds [%s]. Publish it with "+
			"peft/build_gguf.sh and restart the llamacpp container",
			ErrAdapterNotLoaded, name, strings.Join(have, ", "))
	}

	// Measured rather than assumed. The whole claim being made to the operator
	// is that this is instant, and a number they can read is the only version
	// of that claim worth printing. It also catches the regression where a swap
	// starts blocking on a busy slot.
	started := time.Now()
	if _, err := r.do(ctx, http.MethodPost, "/lora-adapters", body); err != nil {
		return 0, err
	}
	return time.Since(started), nil
}

// Active returns the name of the adapter currently applied, or "" for none.
//
// Read back from the runtime rather than from the database on purpose: the
// server forgets every scale when it restarts, so the database records intent
// and this records fact. The panel shows both, because the case where they
// disagree is exactly the one an operator needs to see.
func (r *AdapterRuntime) Active(ctx context.Context) (string, error) {
	loaded, err := r.List(ctx)
	if err != nil {
		return "", err
	}
	for _, a := range loaded {
		if a.Scale > 0 {
			return a.Name, nil
		}
	}
	return "", nil
}

// baseName is filepath.Base without the OS-dependent separator handling. The
// paths come from a Linux container regardless of what this binary runs on, so
// using filepath here would split on backslashes when the backend runs on
// Windows and quietly stop matching.
func baseName(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}
