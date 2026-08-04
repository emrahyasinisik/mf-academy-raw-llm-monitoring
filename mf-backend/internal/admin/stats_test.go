package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/emrah/mf-backend/internal/analysis"
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
	if got := matureWeeks(week(15), now); got != 1 {
		t.Fatalf("15 days old = %d weeks, want 1", got)
	}
	// Gelecekteki bir hafta başı negatif dönmemeli.
	if got := matureWeeks(now.AddDate(0, 0, 7).Unix(), now); got != 0 {
		t.Fatalf("future cohort = %d, want 0", got)
	}
}

// Kohort satırı haftaya göre gruplanıyor ama tutunma her üyenin KENDİ kaydından
// ölçülüyor: pazar günü kaydolan biri 4. hafta penceresini ancak hafta başı +
// 34. günde kapatıyor. Hücre 28. günde açılırsa o üye dönmemiş sayılır.
func TestMatureWeeksWaitsForTheWeeksLastSignup(t *testing.T) {
	weekStart := time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC) // pazartesi
	at := func(days int) time.Time { return weekStart.AddDate(0, 0, days) }

	if got := matureWeeks(weekStart.Unix(), at(33)); got >= 4 {
		t.Fatalf("33rd day = %d mature weeks, want < 4: the Sunday signup still has a day", got)
	}
	if got := matureWeeks(weekStart.Unix(), at(35)); got != 4 {
		t.Fatalf("35th day = %d mature weeks, want 4", got)
	}
	// 2. hafta penceresi (kendi kaydından 7-14 gün) en geç hafta başı + 21'de kapanır.
	if got := matureWeeks(weekStart.Unix(), at(20)); got >= 2 {
		t.Fatalf("20th day = %d mature weeks, want < 2", got)
	}
	if got := matureWeeks(weekStart.Unix(), at(21)); got != 2 {
		t.Fatalf("21st day = %d mature weeks, want 2", got)
	}
}

func score(v float64) *float64 { return &v }

func rated(key string, value float64) analysis.Finding {
	return analysis.Finding{Key: key, EvidenceFound: true, Score: &value}
}

func trialRubric() []analysis.Criterion {
	return []analysis.Criterion{
		{Key: "traction", Weight: 0.5, ScaleMax: 5},
		{Key: "team", Weight: 0.5, ScaleMax: 5},
	}
}

// Sıfır fark, ölçülebilecek EN İYİ tutarlılık sonucudur. Ölçülemeyen bir grup
// için basılırsa kart hiç yapılmamış bir ölçümü kusursuz ilan eder.
func TestConsistencyCardIsAbsentWhenThereIsNoSpreadToMeasure(t *testing.T) {
	rubric := trialRubric()
	one := []trialLeg{{Group: "g", CreatedAt: time.Now(), Score: score(61.5), Criteria: rubric}}
	if card := consistencyCard(one); card != nil {
		t.Fatalf("single run = %+v, want nil: one observation has no spread", card)
	}
	if card := consistencyCard(nil); card != nil {
		t.Fatalf("no runs = %+v, want nil", card)
	}

	// overall_score her bacakta NULL: hiçbir kriter puanlanamamış grup.
	unscored := []trialLeg{
		{Group: "g", CreatedAt: time.Now(), Criteria: rubric},
		{Group: "g", CreatedAt: time.Now(), Criteria: rubric},
	}
	if card := consistencyCard(unscored); card != nil {
		t.Fatalf("all-NULL scores = %+v, want nil, not a 0 puan spread", card)
	}
}

func TestConsistencyCardReportsSpreadAndRedactedLegs(t *testing.T) {
	at := time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC)
	rubric := trialRubric()
	card := consistencyCard([]trialLeg{
		{Group: "g", CreatedAt: at, Score: score(61.5), Criteria: rubric,
			Findings: []analysis.Finding{rated("traction", 2), rated("team", 4)}},
		{Group: "g", CreatedAt: at.Add(time.Minute), Score: score(68.75), Criteria: rubric,
			Findings: []analysis.Finding{rated("traction", 4), rated("team", 4)}},
		// Redakte edilmiş bacak: puanı duruyor, bulguları silinmiş.
		{Group: "g", CreatedAt: at.Add(2 * time.Minute), Score: score(65), Criteria: rubric,
			Redacted: true, Findings: []analysis.Finding{}},
	})
	if card == nil {
		t.Fatal("card = nil, want a measured spread")
	}
	if card.Runs != 3 || card.RedactedRuns != 1 {
		t.Fatalf("runs = %d, redacted = %d; want 3 and 1", card.Runs, card.RedactedRuns)
	}
	if card.MinTotal != 61.5 || card.MaxTotal != 68.75 || card.TotalSpread != 7.25 {
		t.Fatalf("spread = %v (%v..%v), want 7.25 (61.5..68.75)",
			card.TotalSpread, card.MinTotal, card.MaxTotal)
	}
	if card.CreatedAt != at {
		t.Fatalf("created_at = %v, want the group's oldest leg %v", card.CreatedAt, at)
	}
	// team iki koşuda da 4; traction 2 ve 4. Popülasyon sapması, örneklem
	// değil: /analysis/trials/{group} ile aynı sayı basılmalı.
	if card.VolatileCriterion != "traction" {
		t.Fatalf("volatile criterion = %q, want traction", card.VolatileCriterion)
	}
	want := analysis.PerCriterionStdDev(rubric, [][]analysis.Finding{
		{rated("traction", 2), rated("team", 4)},
		{rated("traction", 4), rated("team", 4)},
		{},
	})["traction"]
	if card.VolatileStdDev == nil || *card.VolatileStdDev != want {
		t.Fatalf("volatile stddev = %v, want %v — the panel and the trial endpoint "+
			"must publish one number for one measurement", card.VolatileStdDev, want)
	}
	if want != 0.2 {
		t.Fatalf("population stddev of 0.4 and 0.8 = %v, want 0.2 (stddev_samp would give ~0.283)", want)
	}
}

func TestMostVolatilePicksTheWidestSpreadAndBreaksTiesOnKey(t *testing.T) {
	key, sd := mostVolatile(map[string]float64{
		"traction":    0.12,
		"team":        0.31,
		"market_size": 0.31,
		"risk":        0.05,
	})
	if key != "market_size" {
		t.Fatalf("criterion = %q, want market_size: a tie must resolve on the key, "+
			"or the card alternates between two page loads", key)
	}
	if sd == nil || *sd != 0.31 {
		t.Fatalf("stddev = %v, want 0.31", sd)
	}

	// Ölçülmemiş bir sapma sıfır DEĞİL yokluktur: 0,0000 "mükemmel kararlı" okunur.
	key, sd = mostVolatile(map[string]float64{})
	if key != "" || sd != nil {
		t.Fatalf("empty spread = (%q, %v), want (\"\", nil)", key, sd)
	}
	if key, sd = mostVolatile(nil); key != "" || sd != nil {
		t.Fatalf("nil spread = (%q, %v), want (\"\", nil)", key, sd)
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
	stddev := 0.1123
	store := &fakeStatsStore{res: StatsResponse{Consistency: &ConsistencyCard{
		Group:             "11111111-1111-1111-1111-111111111111",
		Runs:              5,
		CreatedAt:         createdAt,
		TotalSpread:       7.25,
		MinTotal:          61.5,
		MaxTotal:          68.75,
		VolatileCriterion: "traction",
		VolatileStdDev:    &stddev,
		RedactedRuns:      2,
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
	got, want := res.Consistency, store.res.Consistency
	if got == nil {
		t.Fatal("consistency = nil, want card")
	}
	// Alan alan: VolatileStdDev artık pointer, ve == karşılaştırması JSON'dan
	// dönen kartta adresleri kıyaslar.
	if got.Group != want.Group || got.Runs != want.Runs || !got.CreatedAt.Equal(want.CreatedAt) ||
		got.TotalSpread != want.TotalSpread || got.MinTotal != want.MinTotal ||
		got.MaxTotal != want.MaxTotal || got.VolatileCriterion != want.VolatileCriterion ||
		got.RedactedRuns != want.RedactedRuns {
		t.Fatalf("consistency = %+v, want %+v", got, want)
	}
	if got.VolatileStdDev == nil || *got.VolatileStdDev != stddev {
		t.Fatalf("volatile_std_dev = %v, want %v", got.VolatileStdDev, stddev)
	}
}

func TestStatsSerializesUnmeasuredDeviationAsNull(t *testing.T) {
	store := &fakeStatsStore{res: StatsResponse{Consistency: &ConsistencyCard{
		Group: "22222222-2222-2222-2222-222222222222", Runs: 2,
	}}}
	h := &Handler{stats: store}
	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	w := httptest.NewRecorder()

	statsRouter(h).ServeHTTP(w, req)

	var body struct {
		Consistency map[string]json.RawMessage `json:"consistency"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got := string(body.Consistency["volatile_std_dev"]); got != "null" {
		t.Fatalf("volatile_std_dev = %s, want null", got)
	}
	if _, ok := body.Consistency["redacted_runs"]; !ok {
		t.Fatal("redacted_runs must be on the wire, or the panel cannot say how many legs lost their findings")
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
