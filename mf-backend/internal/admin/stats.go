package admin

import (
	"net/http"
	"time"

	"github.com/emrah/mf-backend/internal/common"
)

// One endpoint keeps the dashboard honest: six chart requests mean one timeout
// can leave the panel half full and half empty, which reads as data rather than
// as a failed load.
type StatBox struct {
	Value     float64  `json:"value"`
	Previous  float64  `json:"previous"`
	ChangePct *float64 `json:"change_pct"` // Previous == 0 iken nil: taban yoksa yüzde uydurmuyoruz
}

// ActiveAdapterBox carries the name and nothing else.
//
// It used to carry the window's schema-validity rate too, and that was a
// mislabel rather than a rounding error: the rate is an average over every
// assessment in the window with no join to the adapter that produced it, while
// the box is headed with one adapter's name. Over ninety days that spans
// several builds and the base model, so a reader invited to roll an adapter
// back was reading a figure that mostly measured other adapters. The honest
// split is below; attributing the rate per adapter needs a filter on
// adapter_id and a window scoped to the activation, which is more than this
// endpoint should decide.
type ActiveAdapterBox struct {
	Name string `json:"name"` // "" = aktif adapter yok
}

// SchemaValidityBox is the whole window's schema compliance, every adapter and
// the base model together. Named for what it measures, not for whoever happens
// to be active while it is read.
type SchemaValidityBox struct {
	Rate         float64  `json:"rate"`          // 0..1, pencere içi şema uyumu
	PreviousRate float64  `json:"previous_rate"` // 0..1, önceki pencere
	ChangePoints *float64 `json:"change_points"` // yüzde PUANI farkı; iki pencereden biri boşsa nil
}

type StatsBoxes struct {
	TotalUsers     StatBox           `json:"total_users"`      // Value = şimdiki toplam, Previous = pencere başındaki toplam
	TotalReports   StatBox           `json:"total_reports"`    // aynı mantık, assessments
	ReportsLast24h StatBox           `json:"reports_last_24h"` // Value = son 24s, Previous = ondan önceki 24s
	ActiveAdapter  ActiveAdapterBox  `json:"active_adapter"`
	SchemaValidity SchemaValidityBox `json:"schema_validity"`
}

type DayPoint struct {
	T               int64 `json:"t"` // UTC gün başı, unix saniye
	NewUsers        int   `json:"new_users"`
	CumulativeUsers int   `json:"cumulative_users"` // pencere öncesi taban + koşan toplam
	Assessments     int   `json:"assessments"`
	SchemaValid     int   `json:"schema_valid"` // o günün geçerli şema sayısı (payı)
}

type SeriesPoint struct {
	T int64   `json:"t"`
	V float64 `json:"v"`
}

type TargetSeries struct {
	Target string        `json:"target"` // "browser" | "server"
	Points []SeriesPoint `json:"points"`
}

type CategoryCount struct {
	Key   string `json:"key"` // "individual" | "company"
	Count int    `json:"count"`
}

type Funnel struct {
	Registered int `json:"registered"` // pencerede kaydolan
	Consented  int `json:"consented"`  // onları içinden koşulları kabul etmiş
	Analyzed   int `json:"analyzed"`   // onların içinden en az bir analiz üretmiş
}

type CohortRow struct {
	WeekStart   int64 `json:"week_start"` // UTC hafta başı, unix saniye
	Size        int   `json:"size"`
	Week2       int   `json:"week_2"`       // 7-14. günlerde analiz üreten
	Week4       int   `json:"week_4"`       // 21-28. günlerde analiz üreten
	MatureWeeks int   `json:"mature_weeks"` // kohortun üstünden geçen tam hafta
}

// ConsistencyCard is the panel's one publishable measurement: how far apart the
// same case scored when it was run repeatedly.
//
// The whole card is nil unless a real spread was measured — see
// latestConsistency. Zero spread is the best result this can report, so
// emitting it for a group that could not be measured would publish a perfect
// consistency figure for a measurement that never happened.
type ConsistencyCard struct {
	Group       string    `json:"group"`
	Runs        int       `json:"runs"`
	CreatedAt   time.Time `json:"created_at"`
	TotalSpread float64   `json:"total_spread"` // en yüksek - en düşük ağırlıklı toplam
	MinTotal    float64   `json:"min_total"`
	MaxTotal    float64   `json:"max_total"`

	// The criterion that moved most across the legs, and by how much. Both nil
	// or both empty: a criterion with no deviation beside it, or a deviation
	// with no criterion, is a number about nothing. The old shape coalesced
	// them to '' and 0 and rendered "En oynak kriter: —" next to a confident
	// "Kriter sapması: 0,0000".
	VolatileCriterion string   `json:"volatile_criterion"`
	VolatileStdDev    *float64 `json:"volatile_std_dev"`

	// RedactedRuns is how many legs have had their findings blanked, by their
	// owner or by the 30-day retention sweep.
	//
	// Runs counts the whole group because score, coverage and schema_valid
	// survive redaction; findings do not, and VolatileStdDev is computed from
	// findings. Three redacted legs of five produce a deviation over two
	// observations that looks exactly like one over five, and after a month
	// this is the normal state of every group — so the shortfall is reported
	// rather than left to pass as a smaller spread. Same reason
	// analysis.TrialResult carries redacted_runs.
	RedactedRuns int `json:"redacted_runs"`
}

type StatsResponse struct {
	Window       string           `json:"window"` // "30d" | "90d"
	From         time.Time        `json:"from"`
	To           time.Time        `json:"to"`
	Boxes        StatsBoxes       `json:"boxes"`
	Days         []DayPoint       `json:"days"` // pencerenin HER günü, boş günler sıfırla
	OrgTypes     []CategoryCount  `json:"org_types"`
	RunsByTarget []TargetSeries   `json:"runs_by_target"`
	Funnel       Funnel           `json:"funnel"`
	Cohorts      []CohortRow      `json:"cohorts"`
	Consistency  *ConsistencyCard `json:"consistency"` // trial grubu yoksa null
}

// Yalnızca iki pencere var, ve serbest bir gün sayısı kabul etmiyoruz: her
// pencere aynı uzunlukta bir "önceki pencere" ile karşılaştırılıyor, ve
// keyfi aralık o karşılaştırmayı kullanıcıya sessizce anlamsızlaştırır.
func parseWindow(raw string) (int, string, error) {
	switch raw {
	case "", "30d":
		return 30, "30d", nil
	case "90d":
		return 90, "90d", nil
	default:
		return 0, "", common.ErrBadRequest("window must be 30d or 90d")
	}
}

// Stats panelin tamamını tek yanıtta verir. GET /admin/stats?window=30d|90d
func (h *Handler) Stats(w http.ResponseWriter, r *http.Request) {
	days, label, err := parseWindow(r.URL.Query().Get("window"))
	if err != nil {
		common.Error(w, err)
		return
	}
	// Gün sınırına hizala: kova sınırı isteğin saatine göre kayarsa aynı gün
	// iki ardışık istekte farklı sayı gösterir.
	to := time.Now().UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
	from := to.Add(-time.Duration(days) * 24 * time.Hour)

	// Kendi context'ini KURMA: alt context ebeveyninin süresini uzatamaz, ve
	// /admin yerel grubu zaten REQUEST_TIMEOUT altında.
	res, err := h.stats.Stats(r.Context(), from, to)
	if err != nil {
		common.Error(w, common.ErrInternal("could not read stats"))
		return
	}
	res.Window, res.From, res.To = label, from, to
	// Boş dilim null olarak serileşir ve frontend'in .map çağrısı patlar. Bunu
	// store'a bırakmıyoruz: sözleşme uçta, her store yolunda geçerli olmalı.
	normalizeStats(&res)
	common.JSON(w, http.StatusOK, res)
}

// normalizeStats nil dilimleri boş dilime çevirir.
func normalizeStats(res *StatsResponse) {
	if res.Days == nil {
		res.Days = []DayPoint{}
	}
	if res.OrgTypes == nil {
		res.OrgTypes = []CategoryCount{}
	}
	if res.RunsByTarget == nil {
		res.RunsByTarget = []TargetSeries{}
	}
	if res.Cohorts == nil {
		res.Cohorts = []CohortRow{}
	}
}
