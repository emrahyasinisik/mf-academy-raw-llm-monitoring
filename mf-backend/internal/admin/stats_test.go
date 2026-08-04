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

func TestDaySpineCoversEveryDayOfWindow(t *testing.T) {
	to := time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC)
	from := to.AddDate(0, 0, -30)
	spine := daySpine(from, to)
	if len(spine) != 30 {
		t.Fatalf("len = %d, want 30", len(spine))
	}
	if spine[0] != from.Unix() {
		t.Fatalf("first = %d, want %d", spine[0], from.Unix())
	}
	if spine[29] != to.AddDate(0, 0, -1).Unix() {
		t.Fatalf("last = %d, want %d", spine[29], to.AddDate(0, 0, -1).Unix())
	}
}

func TestMatureWeeksCountsOnlyElapsedWeeks(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	week := func(d int) int64 { return now.AddDate(0, 0, -d).Truncate(24 * time.Hour).Unix() }

	if got := matureWeeks(week(3), now); got != 0 {
		t.Fatalf("3 days old = %d weeks, want 0", got)
	}
	if got := matureWeeks(week(15), now); got != 2 {
		t.Fatalf("15 days old = %d weeks, want 2", got)
	}
	// Gelecekteki bir hafta başı negatif dönmemeli.
	if got := matureWeeks(now.AddDate(0, 0, 7).Unix(), now); got != 0 {
		t.Fatalf("future cohort = %d, want 0", got)
	}
}

func TestAssembleDaysZeroFillsAndAccumulates(t *testing.T) {
	to := time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC)
	from := to.AddDate(0, 0, -3)
	spine := daySpine(from, to)
	newUsers := map[int64]int{spine[0]: 2, spine[2]: 1} // ortadaki gün boş
	days := assembleDays(spine, 10, newUsers, map[int64]int{spine[2]: 5}, map[int64]int{spine[2]: 4})

	if len(days) != 3 {
		t.Fatalf("len = %d, want 3", len(days))
	}
	if days[1].NewUsers != 0 || days[1].Assessments != 0 {
		t.Fatalf("gap day must be zero, got %+v", days[1])
	}
	want := []int{12, 12, 13}
	for i, w := range want {
		if days[i].CumulativeUsers != w {
			t.Fatalf("day %d cumulative = %d, want %d", i, days[i].CumulativeUsers, w)
		}
	}
	if days[2].SchemaValid != 4 || days[2].Assessments != 5 {
		t.Fatalf("day 2 = %+v", days[2])
	}
}

func TestChangePctIsNilWithoutABaseline(t *testing.T) {
	if got := changePct(5, 0); got != nil {
		t.Fatalf("no baseline must give nil, got %v", *got)
	}
	got := changePct(150, 100)
	if got == nil || *got != 50 {
		t.Fatalf("want +50, got %v", got)
	}
	down := changePct(50, 100)
	if down == nil || *down != -50 {
		t.Fatalf("want -50, got %v", down)
	}
}

func TestAssembleTargetsZeroFillsEachTarget(t *testing.T) {
	to := time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC)
	spine := daySpine(to.AddDate(0, 0, -2), to)
	series := assembleTargets(spine, map[string]map[int64]int{
		"server":  {spine[1]: 3},
		"browser": {spine[0]: 1},
	})
	if len(series) != 2 || series[0].Target != "browser" {
		t.Fatalf("targets must be sorted for a stable colour order: %+v", series)
	}
	for _, s := range series {
		if len(s.Points) != 2 {
			t.Fatalf("%s: %d points, want 2", s.Target, len(s.Points))
		}
	}
	if series[1].Points[1].V != 3 {
		t.Fatalf("server day 2 = %v, want 3", series[1].Points[1].V)
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

func TestStatsSerializesMissingConsistencyAsNull(t *testing.T) {
	store := &fakeStatsStore{res: StatsResponse{Consistency: nil}}
	h := &Handler{stats: store}
	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	w := httptest.NewRecorder()

	statsRouter(h).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var body map[string]json.RawMessage
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got := string(body["consistency"]); got != "null" {
		t.Fatalf("consistency = %s, want null", got)
	}
}

func TestStatsSerializesConsistencyCard(t *testing.T) {
	createdAt := time.Date(2026, 8, 4, 12, 30, 0, 0, time.UTC)
	store := &fakeStatsStore{res: StatsResponse{Consistency: &ConsistencyCard{
		Group:             "11111111-1111-1111-1111-111111111111",
		Runs:              5,
		CreatedAt:         createdAt,
		TotalSpread:       7.25,
		MinTotal:          61.5,
		MaxTotal:          68.75,
		VolatileCriterion: "traction",
		VolatileStdDev:    0.1123,
	}}}
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
	if res.Consistency == nil {
		t.Fatal("consistency = nil, want card")
	}
	if *res.Consistency != *store.res.Consistency {
		t.Fatalf("consistency = %+v, want %+v", res.Consistency, store.res.Consistency)
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
