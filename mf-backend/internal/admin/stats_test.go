package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

type fakeStatsStore struct {
	from, to time.Time
	res      StatsResponse
	err      error
}

func (f *fakeStatsStore) Stats(_ context.Context, from, to time.Time) (StatsResponse, error) {
	f.from, f.to = from, to
	return f.res, f.err
}

func statsRouter(h *Handler) http.Handler {
	r := chi.NewRouter()
	r.Get("/stats", h.Stats)
	return r
}

func TestParseWindow(t *testing.T) {
	for _, tc := range []struct {
		raw     string
		days    int
		label   string
		wantErr bool
	}{
		{"", 30, "30d", false},
		{"30d", 30, "30d", false},
		{"90d", 90, "90d", false},
		{"7d", 0, "", true},
		{"30", 0, "", true},
		{"abc", 0, "", true},
	} {
		days, label, err := parseWindow(tc.raw)
		if (err != nil) != tc.wantErr {
			t.Fatalf("%q: err = %v, wantErr %v", tc.raw, err, tc.wantErr)
		}
		if err == nil && (days != tc.days || label != tc.label) {
			t.Fatalf("%q: got (%d, %q), want (%d, %q)", tc.raw, days, label, tc.days, tc.label)
		}
	}
}

func TestStatsDefaultsToThirtyDayWindow(t *testing.T) {
	store := &fakeStatsStore{}
	h := &Handler{stats: store}
	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	w := httptest.NewRecorder()

	statsRouter(h).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var res StatsResponse
	if err := json.NewDecoder(w.Body).Decode(&res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res.Window != "30d" {
		t.Fatalf("window = %q, want 30d", res.Window)
	}
	if got := store.to.Sub(store.from); got != 30*24*time.Hour {
		t.Fatalf("store window = %v, want 720h", got)
	}
	// Boş veritabanı sözleşmesi: diziler null değil boş dizi olarak serileşmeli,
	// yoksa frontend .map çağrısında patlar.
	if res.Days == nil || res.OrgTypes == nil || res.RunsByTarget == nil || res.Cohorts == nil {
		t.Fatalf("empty store must produce empty slices, not null: %+v", res)
	}
}

func TestStatsRejectsUnknownWindow(t *testing.T) {
	h := &Handler{stats: &fakeStatsStore{}}
	req := httptest.NewRequest(http.MethodGet, "/stats?window=7d", nil)
	w := httptest.NewRecorder()

	statsRouter(h).ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestStatsPassesNinetyDayWindowToStore(t *testing.T) {
	store := &fakeStatsStore{}
	h := &Handler{stats: store}
	req := httptest.NewRequest(http.MethodGet, "/stats?window=90d", nil)
	w := httptest.NewRecorder()

	statsRouter(h).ServeHTTP(w, req)

	if got := store.to.Sub(store.from); got != 90*24*time.Hour {
		t.Fatalf("store window = %v, want 2160h", got)
	}
}
