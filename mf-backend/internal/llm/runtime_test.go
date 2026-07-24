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
)

// fakeRuntime stands in for llama-server's /lora-adapters pair. It records the
// last POST body so the tests can assert on what was actually sent, which is
// the part that matters: the bug this guards against is sending a partial
// update that leaves a second adapter applied.
type fakeRuntime struct {
	adapters []LoadedAdapter
	lastPost []scaleEntry
	posts    int
}

func (f *fakeRuntime) server(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/lora-adapters", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			// The real server does not send `name`; it is derived client-side.
			type wire struct {
				ID    int     `json:"id"`
				Path  string  `json:"path"`
				Scale float64 `json:"scale"`
			}
			out := make([]wire, 0, len(f.adapters))
			for _, a := range f.adapters {
				out = append(out, wire{a.ID, a.Path, a.Scale})
			}
			_ = json.NewEncoder(w).Encode(out)
		case http.MethodPost:
			f.posts++
			var body []scaleEntry
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("runtime got undecodable POST body: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			f.lastPost = body
			for _, e := range body {
				for i := range f.adapters {
					if f.adapters[i].ID == e.ID {
						f.adapters[i].Scale = e.Scale
					}
				}
			}
			_ = json.NewEncoder(w).Encode(body)
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func newTestRuntime(t *testing.T, f *fakeRuntime) *AdapterRuntime {
	t.Helper()
	return NewAdapterRuntime(f.server(t).URL, "secret", 5*time.Second)
}

func TestListDerivesNameFromPath(t *testing.T) {
	f := &fakeRuntime{adapters: []LoadedAdapter{
		{ID: 0, Path: "/models/gguf/adapters/tuned-v1.gguf", Scale: 0},
		{ID: 1, Path: "/models/gguf/adapters/tuned-v2.gguf", Scale: 0},
	}}
	got, err := newTestRuntime(t, f).List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 adapters, got %d", len(got))
	}
	// The database stores bare names, so this mapping is what lets a row be
	// matched to a loaded adapter at all.
	if got[0].Name != "tuned-v1.gguf" || got[1].Name != "tuned-v2.gguf" {
		t.Fatalf("names not derived from paths: %+v", got)
	}
}

// The central correctness property: activating one adapter must explicitly zero
// every other one. Sending only the winner would leave the previous adapter
// applied and silently stack two fine-tunes.
func TestActivateZeroesEveryOtherAdapter(t *testing.T) {
	f := &fakeRuntime{adapters: []LoadedAdapter{
		{ID: 0, Path: "/a/one.gguf", Scale: 1},
		{ID: 1, Path: "/a/two.gguf", Scale: 0},
		{ID: 2, Path: "/a/three.gguf", Scale: 0},
	}}
	rt := newTestRuntime(t, f)

	if _, err := rt.Activate(context.Background(), "two.gguf"); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	if len(f.lastPost) != 3 {
		t.Fatalf("want a scale for all 3 adapters, got %d: %+v", len(f.lastPost), f.lastPost)
	}
	want := map[int]float64{0: 0, 1: 1, 2: 0}
	for _, e := range f.lastPost {
		if want[e.ID] != e.Scale {
			t.Errorf("adapter %d: want scale %v, got %v", e.ID, want[e.ID], e.Scale)
		}
	}
}

func TestActivateAcceptsAFullPath(t *testing.T) {
	f := &fakeRuntime{adapters: []LoadedAdapter{{ID: 0, Path: "/a/one.gguf"}}}
	// Callers hold bare names, but a path should not be a failure — it is the
	// same adapter said a different way.
	if _, err := newTestRuntime(t, f).Activate(context.Background(), "/somewhere/one.gguf"); err != nil {
		t.Fatalf("Activate with a path: %v", err)
	}
}

func TestActivateEmptyNameServesTheBase(t *testing.T) {
	f := &fakeRuntime{adapters: []LoadedAdapter{
		{ID: 0, Path: "/a/one.gguf", Scale: 1},
		{ID: 1, Path: "/a/two.gguf", Scale: 0},
	}}
	rt := newTestRuntime(t, f)

	if _, err := rt.Activate(context.Background(), ""); err != nil {
		t.Fatalf("Activate(\"\"): %v", err)
	}
	for _, e := range f.lastPost {
		if e.Scale != 0 {
			t.Fatalf("deactivation left adapter %d at scale %v", e.ID, e.Scale)
		}
	}

	active, err := rt.Active(context.Background())
	if err != nil {
		t.Fatalf("Active: %v", err)
	}
	if active != "" {
		t.Fatalf("want no active adapter, got %q", active)
	}
}

// An adapter that exists in the database but was not loaded at startup is the
// most likely real failure, because publishing a file and restarting the
// container are two separate steps. It must not report success.
func TestActivateUnknownAdapterIsNotSilent(t *testing.T) {
	f := &fakeRuntime{adapters: []LoadedAdapter{{ID: 0, Path: "/a/one.gguf"}}}
	_, err := newTestRuntime(t, f).Activate(context.Background(), "missing.gguf")

	if !errors.Is(err, ErrAdapterNotLoaded) {
		t.Fatalf("want ErrAdapterNotLoaded, got %v", err)
	}
	if f.posts != 0 {
		t.Errorf("nothing should have been posted for an unknown adapter, got %d posts", f.posts)
	}
	// The message has to say what *is* loaded, or the operator's next step is a
	// guess.
	if !strings.Contains(err.Error(), "one.gguf") {
		t.Errorf("error should list the loaded adapters, got: %v", err)
	}
}

func TestActiveReadsBackFromTheRuntime(t *testing.T) {
	f := &fakeRuntime{adapters: []LoadedAdapter{
		{ID: 0, Path: "/a/one.gguf", Scale: 0},
		{ID: 1, Path: "/a/two.gguf", Scale: 1},
	}}
	got, err := newTestRuntime(t, f).Active(context.Background())
	if err != nil {
		t.Fatalf("Active: %v", err)
	}
	if got != "two.gguf" {
		t.Fatalf("want two.gguf, got %q", got)
	}
}

// A 404 means the llamacpp service is not in the stack. That is an operational
// state with an obvious fix, so it must be distinguishable from a swap that was
// attempted and refused.
func TestMissingRuntimeIsReportedAsUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)

	_, err := NewAdapterRuntime(srv.URL, "", time.Second).Activate(context.Background(), "x.gguf")
	if !errors.Is(err, ErrRuntimeUnavailable) {
		t.Fatalf("want ErrRuntimeUnavailable, got %v", err)
	}
	if !strings.Contains(err.Error(), "llamacpp") {
		t.Errorf("error should name the service to start, got: %v", err)
	}
}

func TestUnconfiguredRuntimeRefusesRatherThanPanics(t *testing.T) {
	var rt *AdapterRuntime
	if rt.Configured() {
		t.Fatal("a nil runtime must not report itself configured")
	}
	if _, err := rt.Activate(context.Background(), "x.gguf"); !errors.Is(err, ErrRuntimeUnavailable) {
		t.Fatalf("want ErrRuntimeUnavailable, got %v", err)
	}
}

// The backend can run on Windows while the runtime always reports Linux
// container paths. filepath.Base would split on a backslash there and stop
// matching names that legitimately contain one.
func TestBaseNameIgnoresBackslashes(t *testing.T) {
	if got := baseName(`/models/gguf/odd\name.gguf`); got != `odd\name.gguf` {
		t.Fatalf("want the whole final segment, got %q", got)
	}
	if got := baseName("bare.gguf"); got != "bare.gguf" {
		t.Fatalf("a bare name should pass through, got %q", got)
	}
}
