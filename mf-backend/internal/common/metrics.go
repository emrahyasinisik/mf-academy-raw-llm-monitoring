package common

import (
	"crypto/subtle"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Prometheus instrumentation for the HTTP surface.
//
// The metric that matters operationally is not the count but the shape of the
// latency distribution, so duration is a histogram rather than a gauge of the
// last value or a running average: percentiles cannot be recovered from a mean,
// and a mean hides exactly the tail that wakes people up.

// httpRequests counts requests by route, method and status class.
var httpRequests = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Total HTTP requests, by route pattern, method and status.",
	},
	[]string{"route", "method", "status"},
)

// httpDuration observes request latency.
//
// Buckets are chosen for this service rather than left at the client library's
// defaults: almost every route is a single database query answering in single
// digit milliseconds, while one route waits on a GPU across a tunnel and takes
// seconds. Default buckets would put nearly every observation in one bucket and
// tell us nothing about either population.
var httpDuration = promauto.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "HTTP request latency, by route pattern and method.",
		Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 25},
	},
	[]string{"route", "method"},
)

// httpInFlight is how many requests are being served right now. It is what
// distinguishes "slow" from "overloaded" when latency rises.
var httpInFlight = promauto.NewGauge(
	prometheus.GaugeOpts{
		Name: "http_requests_in_flight",
		Help: "Requests currently being served.",
	},
)

// Metrics records one observation per request.
//
// Labels carry the chi *route pattern* ("/llm/runs/{id}"), never the raw path.
// A raw path would mint a new time series for every run id anyone ever fetches,
// and unbounded label cardinality is the standard way to destroy a Prometheus
// server. The same reasoning keeps user ids out entirely.
func Metrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		ww := wrapStatus(w)

		httpInFlight.Inc()
		defer httpInFlight.Dec()

		next.ServeHTTP(ww, r)

		// Read after the handler: chi only fills the pattern in once routing has
		// happened. An unmatched request has none, and "" would be a silent
		// bucket for every 404, so it is named.
		route := chi.RouteContext(r.Context()).RoutePattern()
		if route == "" {
			route = "unmatched"
		}

		httpRequests.WithLabelValues(route, r.Method, strconv.Itoa(ww.status)).Inc()
		httpDuration.WithLabelValues(route, r.Method).Observe(time.Since(started).Seconds())
	})
}

// MetricsHandler serves the Prometheus exposition, guarded by a bearer token
// when one is configured.
//
// The comparison is constant time. A token check that returns early on the
// first wrong byte leaks the token's prefix to anyone willing to time enough
// requests, and there is no reason to hand that away for a shorter function.
func MetricsHandler(token string) http.Handler {
	exposition := promhttp.Handler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token != "" {
			supplied := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if subtle.ConstantTimeCompare([]byte(supplied), []byte(token)) != 1 {
				Error(w, ErrUnauthorized("metrics require a valid token"))
				return
			}
		}
		exposition.ServeHTTP(w, r)
	})
}

// statusWriter captures the status code, which net/http otherwise discards once
// it has been written to the wire.
type statusWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func wrapStatus(w http.ResponseWriter) *statusWriter {
	// 200 is the status a handler that never calls WriteHeader produces.
	return &statusWriter{ResponseWriter: w, status: http.StatusOK}
}

func (w *statusWriter) WriteHeader(code int) {
	if !w.wroteHeader {
		w.status = code
		w.wroteHeader = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	w.wroteHeader = true
	return w.ResponseWriter.Write(b)
}
