package obs

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/emrah/mf-backend/internal/common"
)

// The headers are the contract with the gateway, and one of them is the fix for
// a failure that took a day to find: an anonymous Go client crossing Cloudflare
// gets a challenge page rather than an answer, and the query reports only that
// the store was unreachable.
func TestQueryRangeSendsIdentityAndSecret(t *testing.T) {
	var gotUA, gotKey, gotBearer, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		gotKey = r.Header.Get("X-API-Key")
		gotBearer = r.Header.Get("Authorization")
		gotQuery = r.URL.Query().Get("query")
		_, _ = w.Write([]byte(`{"status":"success","data":{"result":[]}}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "sekret", time.Second)
	if _, err := c.QueryRange(context.Background(), "up", time.Now().Add(-time.Hour), time.Now(), 30*time.Second, ""); err != nil {
		t.Fatalf("QueryRange: %v", err)
	}
	if gotUA != common.UserAgent {
		t.Errorf("User-Agent = %q, want %q — Go's default is what edge bot protection challenges", gotUA, common.UserAgent)
	}
	if gotKey != "sekret" || gotBearer != "Bearer sekret" {
		t.Errorf("auth headers = %q / %q, want both forms of the shared secret", gotKey, gotBearer)
	}
	if gotQuery != "up" {
		t.Errorf("query = %q, want %q", gotQuery, "up")
	}
}

// A refusal has to survive as a refusal. Collapsing it into "unavailable" is
// what left four identical messages on the panel for four different repairs.
func TestQueryRangeKeepsTheStatusOfARefusal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("<!DOCTYPE html><title>Just a moment...</title>"))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "sekret", time.Second)
	_, err := c.QueryRange(context.Background(), "up", time.Now().Add(-time.Hour), time.Now(), 30*time.Second, "")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v, want it to satisfy ErrUnavailable for callers that only branch on that", err)
	}
	var se *StatusError
	if !errors.As(err, &se) {
		t.Fatalf("error = %v, want a *StatusError in the chain", err)
	}
	if se.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", se.Code, http.StatusForbidden)
	}
	if se.Body == "" {
		t.Error("body snippet empty; a challenge page and a gateway rejection are both 403 without it")
	}
}

// A deadline that fires is a different repair from a box that is switched off,
// so it has to stay distinguishable through the wrapping.
func TestQueryRangeKeepsADeadlineDistinguishable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "sekret", 50*time.Millisecond)
	_, err := c.QueryRange(context.Background(), "up", time.Now().Add(-time.Hour), time.Now(), 30*time.Second, "")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v, want it to satisfy ErrUnavailable", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("error = %v, want the deadline to stay reachable through the wrap", err)
	}
}

// NaN is not an edge case here: histogram_quantile returns one for every step
// whose buckets saw nothing, which is most of a quiet night, and encoding/json
// refuses to marshal it. Dropping the point leaves a gap in one line; keeping
// it would fail the whole response.
func TestQueryRangeDropsUnrepresentableSamples(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","data":{"result":[
			{"metric":{"target":"server"},"values":[[1,"0.5"],[2,"NaN"],[3,"+Inf"],[4,"1.5"]]}
		]}}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "sekret", time.Second)
	series, err := c.QueryRange(context.Background(), "q", time.Now().Add(-time.Hour), time.Now(), 30*time.Second, "target")
	if err != nil {
		t.Fatalf("QueryRange: %v", err)
	}
	if len(series) != 1 || series[0].Label != "server" {
		t.Fatalf("series = %+v, want one labelled by the legend label", series)
	}
	if len(series[0].Points) != 2 {
		t.Errorf("points = %+v, want the two finite samples", series[0].Points)
	}
}

// No metrics store configured is not a failure: everything else in the product
// works without one, so the caller degrades rather than erroring at startup.
func TestUnconfiguredClientReportsItself(t *testing.T) {
	c := NewClient("", "", time.Second)
	if c.Configured() {
		t.Error("Configured() = true for an empty base URL")
	}
	if _, err := c.QueryRange(context.Background(), "up", time.Now(), time.Now(), time.Second, ""); !errors.Is(err, ErrUnavailable) {
		t.Errorf("error = %v, want ErrUnavailable", err)
	}
}
