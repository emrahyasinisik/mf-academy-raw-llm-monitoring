// Package obs reads back the metrics this service exports.
//
// The exposition in internal/common is the write side: counters and histograms
// that Prometheus scrapes. This is the read side, and the two never meet in
// process — the numbers travel out over /metrics, are stored by Prometheus, and
// come back over the query API. That round trip is what makes a chart of the
// last day possible at all; the process itself holds only current values.
//
// Prometheus runs beside the GPU, not beside this service, so the query goes
// through the same tunnel and the same shared secret as inference. The gateway
// there forwards only /api/v1/query and /api/v1/query_range.
package obs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ErrUnavailable means Prometheus could not be reached or refused the query.
// Kept distinct from an empty result: "the metrics store is down" and "nothing
// happened in this window" look identical on a chart and mean opposite things.
var ErrUnavailable = errors.New("metrics store unavailable")

// Point is one sample. Time is Unix seconds, which is what the API returns and
// what a chart axis wants; converting to time.Time here would only make the
// JSON heavier for the browser to parse back.
type Point struct {
	T int64   `json:"t"`
	V float64 `json:"v"`
}

// Series is one line on a chart. Label is empty for single-series panels.
type Series struct {
	Label  string  `json:"label"`
	Points []Point `json:"points"`
}

// Client queries a Prometheus HTTP API through the inference gateway.
type Client struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

// NewClient builds a query client. baseURL is the gateway prefix that routes to
// Prometheus (…/prom), not Prometheus itself: it publishes no host port, and
// going through the gateway puts the query behind the same secret as inference.
//
// The timeout is deliberately shorter than the server's per-request bound. If
// the inference box is switched off, a connection here hangs, and failing on
// our own clock lets the endpoint answer "metrics store unavailable" instead of
// the middleware's generic timeout — one of those tells the operator where to
// look.
func NewClient(baseURL, apiKey string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		client:  &http.Client{Timeout: timeout},
	}
}

// Configured reports whether a metrics store was wired at all. Everything else
// in the product works without one, so callers degrade rather than fail.
func (c *Client) Configured() bool { return c != nil && c.baseURL != "" }

// QueryRange runs a range query and returns one Series per result.
//
// legend names the metric label to title each series by. Passing "" — or naming
// a label the result does not carry — yields a single unlabelled series, which
// is the right shape for an aggregate like a total request rate.
func (c *Client) QueryRange(ctx context.Context, expr string, start, end time.Time, step time.Duration, legend string) ([]Series, error) {
	if !c.Configured() {
		return nil, ErrUnavailable
	}

	q := url.Values{}
	q.Set("query", expr)
	// Seconds, not RFC3339: both are accepted, and this form cannot be made
	// ambiguous by a timezone.
	q.Set("start", strconv.FormatInt(start.Unix(), 10))
	q.Set("end", strconv.FormatInt(end.Unix(), 10))
	q.Set("step", strconv.Itoa(int(step.Seconds())))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/api/v1/query_range?"+q.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("build metrics request: %w", err)
	}
	// Both headers, matching the rest of this codebase's calls to the gateway:
	// Caddy checks X-API-Key, and the Authorization form keeps the request
	// usable against an OpenAI-dialect endpoint without a second code path.
	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: query API returned %d", ErrUnavailable, resp.StatusCode)
	}

	// Values arrive as [unixSeconds, "stringValue"] pairs — the timestamp a
	// number, the sample a string, because a float64 cannot carry NaN or Inf
	// through JSON. json.RawMessage lets each half be decoded on its own terms.
	var body struct {
		Status string `json:"status"`
		Data   struct {
			Result []struct {
				Metric map[string]string    `json:"metric"`
				Values [][2]json.RawMessage `json:"values"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if body.Status != "success" {
		return nil, fmt.Errorf("%w: query API status %q", ErrUnavailable, body.Status)
	}

	out := make([]Series, 0, len(body.Data.Result))
	for _, res := range body.Data.Result {
		s := Series{Label: res.Metric[legend], Points: make([]Point, 0, len(res.Values))}
		for _, pair := range res.Values {
			var ts float64
			if err := json.Unmarshal(pair[0], &ts); err != nil {
				continue
			}
			var raw string
			if err := json.Unmarshal(pair[1], &raw); err != nil {
				continue
			}
			// ParseFloat accepts "NaN" and "+Inf" without error, so the check
			// has to be explicit. histogram_quantile returns NaN for any step
			// whose buckets saw no observations, which is most of a quiet
			// night, and encoding/json refuses to marshal one — left in, a
			// single idle minute would fail the whole response rather than
			// leave a gap in one line.
			v, err := strconv.ParseFloat(raw, 64)
			if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
				continue
			}
			s.Points = append(s.Points, Point{T: int64(ts), V: v})
		}
		out = append(out, s)
	}
	return out, nil
}
