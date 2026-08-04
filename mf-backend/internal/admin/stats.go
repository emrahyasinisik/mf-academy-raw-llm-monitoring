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

type ActiveAdapterBox struct {
	Name         string   `json:"name"`          // "" = aktif adapter yok
	ValidRate    float64  `json:"valid_rate"`    // 0..1, pencere içi şema uyumu
	PreviousRate float64  `json:"previous_rate"` // 0..1, önceki pencere
	ChangePoints *float64 `json:"change_points"` // yüzde PUANI farkı; iki pencereden biri boşsa nil
}

type StatsBoxes struct {
	TotalUsers     StatBox          `json:"total_users"`      // Value = şimdiki toplam, Previous = pencere başındaki toplam
	TotalReports   StatBox          `json:"total_reports"`    // aynı mantık, assessments
	ReportsLast24h StatBox          `json:"reports_last_24h"` // Value = son 24s, Previous = ondan önceki 24s
	ActiveAdapter  ActiveAdapterBox `json:"active_adapter"`
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

type ConsistencyCard struct {
	Group             string    `json:"group"`
	Runs              int       `json:"runs"`
	CreatedAt         time.Time `json:"created_at"`
	TotalSpread       float64   `json:"total_spread"` // en yüksek - en düşük ağırlıklı toplam
	MinTotal          float64   `json:"min_total"`
	MaxTotal          float64   `json:"max_total"`
	VolatileCriterion string    `json:"volatile_criterion"`
	VolatileStdDev    float64   `json:"volatile_std_dev"`
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
