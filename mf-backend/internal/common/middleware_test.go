package common

import (
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
