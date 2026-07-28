package admin

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/emrah/mf-backend/internal/common"
	"github.com/emrah/mf-backend/internal/obs"
)

// The admin panel's charts.
//
// The queries live here rather than travelling from the browser, and that is
// the security boundary of this endpoint. An endpoint that forwards arbitrary
// PromQL is a Prometheus console with our authentication in front of it: a
// caller could ask for a year at one-second resolution and take the metrics
// store down, or read series this panel never intended to show. A fixed set
// answers the questions the panel asks and nothing else, so the cost of a
// request is known before it is made.
//
// Grafana is not replaced by this. It stays the operator's tool for asking
// questions nobody anticipated; these four panels are the ones worth having
// without leaving the product.

// MetricsQuerier is the slice of the metrics store this package needs.
type MetricsQuerier interface {
	QueryRange(ctx context.Context, expr string, start, end time.Time, step time.Duration, legend string) ([]obs.Series, error)
	Configured() bool
}

// panel is one chart: a title, a unit the frontend formats by, and the query
// behind it.
type panel struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Unit is "rps", "seconds" or "count" — the frontend formats an axis from
	// this rather than guessing from the magnitude of the numbers, which would
	// change label style as traffic changes.
	Unit string `json:"unit"`
	Help string `json:"help"`

	expr   string
	legend string
}

// The rate window is fixed at 5m rather than scaled to the chart window. It has
// to cover several scrape intervals to mean anything, and widening it with the
// window would smooth away exactly the spike a 24-hour view is being opened to
// find.
var panels = []panel{
	{
		ID:    "http_rate",
		Title: "İstek hızı",
		Unit:  "rps",
		Help:  "Saniyedeki HTTP isteği. Bu servise gelen tüm trafik.",
		expr:  `sum(rate(http_requests_total[5m]))`,
	},
	{
		ID:    "http_errors",
		Title: "Sunucu hatası",
		Unit:  "rps",
		Help:  "Saniyedeki 5xx. Sıfırdan farklıysa sebebi loglarda.",
		expr:  `sum(rate(http_requests_total{status=~"5.."}[5m]))`,
	},
	{
		ID:    "http_p95",
		Title: "p95 gecikme",
		Unit:  "seconds",
		Help:  "İsteklerin %95'i bu sürenin altında bitti.",
		expr:  `histogram_quantile(0.95, sum by (le) (rate(http_request_duration_seconds_bucket[5m])))`,
	},
	{
		// Split by target, never summed. A browser run's latency describes
		// whichever GPU the visitor owns; a server run's describes one fixed
		// card plus the hop to reach it. An average over both is true of
		// neither — the same reason the exposition carries the label at all.
		ID:     "gen_p95",
		Title:  "Üretim p95 (hedefe göre)",
		Unit:   "seconds",
		Help:   "browser = ziyaretçinin kartı, server = kutudaki kart. Toplanmazlar.",
		expr:   `histogram_quantile(0.95, sum by (le, target) (rate(llm_generation_duration_seconds_bucket[5m])))`,
		legend: "target",
	},
}

// window is a chart span and the step that keeps it near 120 points.
//
// Bounded because the step is what decides the cost of the query: a day at the
// one-hour step's resolution would ask Prometheus for 86,400 points per series
// and hand the browser a payload it cannot draw. Points, not pixels — a line
// with more samples than the element has columns is slower to render and looks
// identical.
type window struct {
	span time.Duration
	step time.Duration
}

var windows = map[string]window{
	"1h":  {time.Hour, 30 * time.Second},
	"6h":  {6 * time.Hour, 3 * time.Minute},
	"24h": {24 * time.Hour, 12 * time.Minute},
}

type panelResult struct {
	panel
	Series []obs.Series `json:"series"`
	// Error carries a per-panel failure so one broken query does not blank the
	// page. In practice they fail together — the store is up or it is not — but
	// a typo in one expression should cost one chart.
	Error string `json:"error,omitempty"`
}

type metricsResponse struct {
	Window string        `json:"window"`
	Step   int           `json:"step_seconds"`
	Panels []panelResult `json:"panels"`
}

// Metrics answers the charts for one window.
//
// GET /admin/metrics?window=1h|6h|24h
func (h *Handler) Metrics(w http.ResponseWriter, r *http.Request) {
	if h.metrics == nil || !h.metrics.Configured() {
		// The same degradation as server-side generation: no inference host
		// means no Prometheus behind its gateway, and the panel says so rather
		// than drawing empty axes that look like an idle system.
		common.Error(w, common.ErrUnavailable(
			"metrik deposu yapılandırılmadı — LLM_BASE_URL boş"))
		return
	}

	key := r.URL.Query().Get("window")
	win, ok := windows[key]
	if !ok {
		key, win = "1h", windows["1h"]
	}

	end := time.Now()
	start := end.Add(-win.span)

	results := make([]panelResult, len(panels))
	var wg sync.WaitGroup
	for i, p := range panels {
		wg.Add(1)
		go func(i int, p panel) {
			defer wg.Done()
			results[i] = panelResult{panel: p}
			series, err := h.metrics.QueryRange(r.Context(), p.expr, start, end, win.step, p.legend)
			if err != nil {
				// Four sequential round trips to a tunnelled host would not fit
				// the request budget, so they run together; each writes its own
				// slot and nothing is shared, which is what makes that safe
				// without a mutex.
				results[i].Error = errText(err)
				return
			}
			results[i].Series = series
		}(i, p)
	}
	wg.Wait()

	common.JSON(w, http.StatusOK, metricsResponse{
		Window: key,
		Step:   int(win.step.Seconds()),
		Panels: results,
	})
}

func errText(err error) string {
	if errors.Is(err, obs.ErrUnavailable) {
		return "metrik deposuna ulaşılamadı"
	}
	return "sorgu başarısız"
}
