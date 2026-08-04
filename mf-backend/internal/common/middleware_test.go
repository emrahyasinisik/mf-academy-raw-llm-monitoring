package common

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// The whole point of the Timeout middleware is that the deadline reaches
// whatever the handler calls — in production, pgx. If the handler's context
// carries no deadline, a slow query holds its pooled connection until the
// client disconnects, and enough of those exhaust the pool.
func TestTimeoutSetsDeadlineOnHandlerContext(t *testing.T) {
	var (
		gotDeadline bool
		deadline    time.Time
	)
	h := Timeout(50 * time.Millisecond)(http.HandlerFunc(
		func(_ http.ResponseWriter, r *http.Request) {
			deadline, gotDeadline = r.Context().Deadline()
		}))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/llm/runs", nil))

	if !gotDeadline {
		t.Fatal("handler context has no deadline; it would never cancel a slow query")
	}
	if d := time.Until(deadline); d <= 0 || d > 50*time.Millisecond {
		t.Errorf("deadline is %v out, want within (0, 50ms]", d)
	}
}

// Work started by a handler must observe cancellation once the budget is spent.
func TestTimeoutCancelsOverrunningHandler(t *testing.T) {
	errCh := make(chan error, 1)
	h := Timeout(20 * time.Millisecond)(http.HandlerFunc(
		func(_ http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
			errCh <- r.Context().Err()
		}))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/llm/metrics", nil))

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("context error is nil, want a deadline error")
		}
	case <-time.After(time.Second):
		t.Fatal("handler was never cancelled")
	}
}

func TestRequirePasswordFresh(t *testing.T) {
	nextCalled := false
	h := RequirePasswordFresh(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusNoContent)
	}))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = r.WithContext(ContextWithClaims(r.Context(), AuthClaims{
		UserID:        "u1",
		PasswordReset: true,
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", w.Code, w.Body.String())
	}
	if nextCalled {
		t.Fatal("next handler ran despite password reset requirement")
	}
	var body ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("error body did not decode: %v", err)
	}
	if body.Error != "password_change_required" {
		t.Fatalf("error code = %q, want password_change_required", body.Error)
	}

	r = httptest.NewRequest(http.MethodGet, "/", nil)
	r = r.WithContext(ContextWithClaims(r.Context(), AuthClaims{UserID: "u1"}))
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("fresh status = %d, want 204; body=%s", w.Code, w.Body.String())
	}
	if !nextCalled {
		t.Fatal("next handler did not run for fresh password")
	}
}
