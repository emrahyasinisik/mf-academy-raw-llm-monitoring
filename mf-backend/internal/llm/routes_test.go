package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/emrah/mf-backend/internal/common"
)

// These guard the request budget each route gets, which is invisible in any
// response and so went wrong unnoticed: generation was mounted under a 5s
// bound, and because a child context cannot extend a parent's deadline, the
// longer bound declared on the route never applied. Requests were cut off at
// exactly 5s with the GPU still generating, and the only symptom was a 504
// that looked like a slow inference host.

const (
	testDefaultTimeout = 5 * time.Second
	testGenTimeout     = 25 * time.Second
)

func okVerifier(string) (common.AuthClaims, error) {
	return common.AuthClaims{UserID: "user-1"}, nil
}

// deadlineSpyStore records the budget the context reaching the store carried.
type deadlineSpyStore struct {
	fakeStore
	budget time.Duration
}

func (s *deadlineSpyStore) ListRuns(ctx context.Context, _, _ string, _ int, _ time.Time) (ListResult, error) {
	if dl, ok := ctx.Deadline(); ok {
		s.budget = time.Until(dl)
	}
	return ListResult{Runs: []RunSummary{}}, nil
}

func TestGenerateRouteGetsTheLongBudgetNotTheShortDefault(t *testing.T) {
	gen := &fakeGen{configured: true, result: Completion{Content: "ok"}}
	h := NewHandler(&fakeStore{}, gen).Routes(okVerifier, testDefaultTimeout, testGenTimeout)

	r := httptest.NewRequest(http.MethodPost, "/generate",
		strings.NewReader(`{"model":"gemma-2-2b-it-q4f16_1-MLC","prompt":"hi"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	// Allows for the milliseconds spent reaching the handler, but the budget
	// must be recognisably the long one rather than the short one.
	if gen.budget < 20*time.Second {
		t.Errorf("generation had %v of budget, want ~%v — a shorter bound is winning",
			gen.budget, testGenTimeout)
	}
}

func TestOtherLLMRoutesKeepTheShortBudget(t *testing.T) {
	spy := &deadlineSpyStore{}
	h := NewHandler(spy, &fakeGen{configured: true}).Routes(okVerifier, testDefaultTimeout, testGenTimeout)

	r := httptest.NewRequest(http.MethodGet, "/runs", nil)
	r.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if spy.budget == 0 {
		t.Fatal("no deadline reached the store: this route is unbounded")
	}
	// The long bound must not leak onto the database routes. It exists to wait
	// on a GPU; a stalled query holding a pooled connection for 25s is the very
	// thing the short default prevents.
	if spy.budget > 10*time.Second {
		t.Errorf("list route had %v of budget, want ~%v — the generation bound has leaked",
			spy.budget, testDefaultTimeout)
	}
}
