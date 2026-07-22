package llm

import (
	"errors"
	"net/http"

	"github.com/emrah/mf-backend/internal/common"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Prometheus instrumentation for generation itself.
//
// These are labelled by target because that is the whole point: a browser run
// and a server run of the same model id measure different machines, and any
// panel that averages them together is showing a number that describes neither.
// Keeping the label here means the dashboard cannot accidentally merge them.
//
// Only server-side generation happens in this process. Browser runs are filed
// from the values the client reports when it posts its result, so those figures
// are a claim rather than our own measurement. The target label is what keeps
// the two from being read as the same kind of number.

var generationDuration = promauto.NewHistogramVec(
	prometheus.HistogramOpts{
		Name: "llm_generation_duration_seconds",
		Help: "Time to produce a completion, by target and model.",
		// Spans both populations: a browser run on a good GPU lands under a
		// second, a server run crosses a tunnel and takes a few.
		Buckets: []float64{0.25, 0.5, 1, 2, 3, 5, 8, 13, 21, 30},
	},
	[]string{"target", "model"},
)

var generationTokens = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "llm_generation_tokens_total",
		Help: "Tokens processed, by target, model and kind (prompt or completion).",
	},
	[]string{"target", "model", "kind"},
)

// generationFailures is labelled by reason rather than by HTTP status because
// the reasons call for different responses: "unavailable" means the inference
// machine is switched off, "timeout" means it is struggling, "upstream" means
// it refused us. An alert worth having distinguishes these.
var generationFailures = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "llm_generation_failures_total",
		Help: "Failed server-side generations, by reason.",
	},
	[]string{"reason"},
)

// recordRun files one completed run. Latency is taken in milliseconds because
// that is the unit the rest of the system carries it in; Prometheus wants
// seconds, and converting in one place beats converting at every call site.
func recordRun(target, model string, latencyMs, promptTokens, completionTokens int) {
	generationDuration.WithLabelValues(target, model).Observe(float64(latencyMs) / 1000)
	generationTokens.WithLabelValues(target, model, "prompt").Add(float64(promptTokens))
	generationTokens.WithLabelValues(target, model, "completion").Add(float64(completionTokens))
}

func recordFailure(reason string) {
	generationFailures.WithLabelValues(reason).Inc()
}

// failureReason maps the error a generation failed with onto a bounded label
// set. Bounded is the operative word: deriving the label from the error message
// would let the upstream mint new time series at will.
func failureReason(err error) string {
	var apiErr *common.APIError
	if !errors.As(err, &apiErr) {
		return "unknown"
	}
	switch apiErr.Status {
	case http.StatusServiceUnavailable:
		return "unavailable"
	case http.StatusGatewayTimeout:
		return "timeout"
	case http.StatusBadGateway:
		return "upstream"
	default:
		return "unknown"
	}
}
