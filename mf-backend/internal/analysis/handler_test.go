package analysis

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/emrah/mf-backend/internal/common"
	"github.com/go-chi/chi/v5"
)

// The default chat budget must not be allowed to truncate a rubric answer.
// A cut-off JSON scores schema_valid=false for reasons that have nothing to do
// with the model, and that bogus figure is what a fine-tuning decision would
// then be made against.
func TestTokenBudgetFloorsAboveTheChatDefault(t *testing.T) {
	// The shipped startup rubric has 9 criteria; the settings default is 512.
	got := tokenBudget(9, 512)
	if got <= 512 {
		t.Errorf("tokenBudget(9, 512) = %d; the default would truncate the answer", got)
	}
	if got < 9*100 {
		t.Errorf("tokenBudget(9, 512) = %d; too small for nine findings with evidence", got)
	}
}

// It is a floor, not an override — a generous operator setting must survive.
func TestTokenBudgetKeepsLargerConfiguredValue(t *testing.T) {
	if got := tokenBudget(3, 2048); got != 2048 {
		t.Errorf("tokenBudget(3, 2048) = %d, want 2048", got)
	}
}

// The card is 6 GB in --mode local; an unbounded budget buys a timeout, not a
// longer answer.
func TestTokenBudgetIsCapped(t *testing.T) {
	if got := tokenBudget(500, 0); got > 4096 {
		t.Errorf("tokenBudget(500, 0) = %d, want it capped at 4096", got)
	}
}

// trialTimeout must cover every sequential run, or a trial is cut off after
// roughly the first generation and reports a spread built from one sample.
func TestTrialTimeoutCoversEveryRun(t *testing.T) {
	gen := time.Second * 25
	got := TrialTimeout(gen)
	if got < gen*maxTrials {
		t.Errorf("TrialTimeout(%v) = %v; too short for %d sequential runs", gen, got, maxTrials)
	}
}

func TestTrialTimeoutIsCapped(t *testing.T) {
	if got := TrialTimeout(time.Second * 600); got > time.Minute*30 {
		t.Errorf("trialTimeout = %v, want capping at 30m", got)
	}
}

// redactStore is the only part of AssessmentStore these tests touch. The rest
// of the interface is embedded so this stays compiling when the interface
// grows, without pretending to implement methods the handler never calls here.
type redactStore struct {
	AssessmentStore
	changed bool
	err     error
	gotUser string
	gotID   string
}

func (s *redactStore) RedactAssessment(_ context.Context, userID, id string) (bool, error) {
	s.gotUser, s.gotID = userID, id
	return s.changed, s.err
}

func deleteRequest(t *testing.T, h *Handler, id string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodDelete, "/analysis/"+id, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	r = r.WithContext(common.ContextWithClaims(
		context.WithValue(r.Context(), chi.RouteCtxKey, rctx),
		common.AuthClaims{UserID: "user-1"}))
	w := httptest.NewRecorder()
	h.Delete(w, r)
	return w
}

func TestDeleteRedactsAndReturns204(t *testing.T) {
	st := &redactStore{changed: true}
	w := deleteRequest(t, NewHandler(st, nil, nil), "rep-1")
	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", w.Code)
	}
	if st.gotUser != "user-1" || st.gotID != "rep-1" {
		t.Errorf("store called with (%q,%q), want (user-1,rep-1)", st.gotUser, st.gotID)
	}
}

// Idempotent on purpose: a second click, a double-submitted form or a retried
// request must not read as a failure. The data is already gone, which is the
// outcome the caller asked for.
func TestDeleteIsIdempotent(t *testing.T) {
	st := &redactStore{changed: false}
	if w := deleteRequest(t, NewHandler(st, nil, nil), "rep-1"); w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204 for an already-redacted report", w.Code)
	}
}

// 404 rather than 403 for someone else's report: a 403 confirms the id exists,
// which is a fact the caller is not entitled to. GetAssessment already answers
// this way and the two must not disagree.
func TestDeleteHidesOtherPeoplesReports(t *testing.T) {
	st := &redactStore{err: ErrNoRows}
	if w := deleteRequest(t, NewHandler(st, nil, nil), "rep-1"); w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// A redacted leg keeps its score but loses its findings, and the per-criterion
// spread is computed from findings. Without a count, a five-run group whose
// spread rests on two observations is indistinguishable from one that rests on
// five — and the spread is the number this endpoint exists to publish.
func TestSummariseCountsRedactedLegs(t *testing.T) {
	score := 70.0
	when := time.Now()
	crit := []Criterion{{Key: "a", Weight: 1, ScaleMax: 5}}

	items := []Assessment{
		{ID: "1", OverallScore: &score, Coverage: 1, SchemaValid: true,
			CriteriaSnapshot: crit,
			Findings:         []Finding{{Key: "a", EvidenceFound: true, Score: &score}}},
		{ID: "2", OverallScore: &score, Coverage: 1, SchemaValid: true,
			CriteriaSnapshot: crit, Findings: []Finding{}, RedactedAt: &when},
		{ID: "3", OverallScore: &score, Coverage: 1, SchemaValid: true,
			CriteriaSnapshot: crit, Findings: []Finding{}, RedactedAt: &when},
	}

	out := summarise("g1", items)
	if out.RedactedRuns != 2 {
		t.Errorf("RedactedRuns = %d, want 2", out.RedactedRuns)
	}
	// The score-side aggregates are untouched by redaction and must still count
	// every leg: score and coverage are never blanked.
	if out.Trials != 3 || out.ScoredRuns != 3 {
		t.Errorf("Trials/ScoredRuns = %d/%d, want 3/3", out.Trials, out.ScoredRuns)
	}
}
