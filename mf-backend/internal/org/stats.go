package org

import (
	"log/slog"
	"math"
	"net/http"
	"sort"
	"time"

	"github.com/emrah/mf-backend/internal/common"
)

// parseWindow accepts only 30d|90d (empty → 30d). Free day counts would break
// the prior-window comparison the boxes rely on — same rule as /admin/stats.
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

// Stats is GET /org/stats?window=30d|90d. Every figure is filtered to members
// of claims.OrgID via Postgres — never Prometheus. GPU downtime must not empty
// a customer's company panel.
func (h *Handler) Stats(w http.ResponseWriter, r *http.Request) {
	claims, ok := common.ClaimsFromContext(r.Context())
	if !ok {
		common.Error(w, common.ErrUnauthorized("authentication required"))
		return
	}

	days, label, err := parseWindow(r.URL.Query().Get("window"))
	if err != nil {
		common.Error(w, err)
		return
	}

	// Align to UTC day boundaries so the same calendar day does not split
	// across two consecutive requests that differ by a few hours.
	to := time.Now().UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
	from := to.Add(-time.Duration(days) * 24 * time.Hour)

	res, err := h.store.Stats(r.Context(), claims.OrgID, from, to)
	if err != nil {
		slog.Error("org stats failed", "error", err, "org_id", claims.OrgID)
		common.Error(w, common.ErrInternal("could not read stats"))
		return
	}
	res.Window, res.From, res.To = label, from, to
	normalizeOrgStats(&res)
	common.JSON(w, http.StatusOK, res)
}

func normalizeOrgStats(res *OrgStats) {
	if res.AssessmentsPerDay == nil {
		res.AssessmentsPerDay = []DayPoint{}
	}
	if res.SchemaValidPerDay == nil {
		res.SchemaValidPerDay = []DayPoint{}
	}
	if res.RunsByTarget == nil {
		res.RunsByTarget = []TargetSeries{}
	}
	if res.MemberActivity == nil {
		res.MemberActivity = []MemberAct{}
	}
}

func daySpine(from, to time.Time) []int64 {
	spine := []int64{}
	for day := from.UTC(); day.Before(to.UTC()); day = day.Add(24 * time.Hour) {
		spine = append(spine, day.Unix())
	}
	return spine
}

func utcDayUnix(t time.Time) int64 {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Unix()
}

func changePct(current, previous float64) *float64 {
	if previous == 0 {
		return nil
	}
	v := math.Round(((current-previous)/previous*100)*100) / 100
	return &v
}

func assembleDaySeries(spine []int64, byDay map[int64]int) []DayPoint {
	out := make([]DayPoint, 0, len(spine))
	for _, t := range spine {
		out = append(out, DayPoint{T: t, V: byDay[t]})
	}
	return out
}

func assembleTargets(spine []int64, byTarget map[string]map[int64]int) []TargetSeries {
	targets := make([]string, 0, len(byTarget))
	for target := range byTarget {
		targets = append(targets, target)
	}
	sort.Strings(targets)

	series := make([]TargetSeries, 0, len(targets))
	for _, target := range targets {
		points := make([]SeriesPoint, 0, len(spine))
		for _, t := range spine {
			points = append(points, SeriesPoint{T: t, V: float64(byTarget[target][t])})
		}
		series = append(series, TargetSeries{Target: target, Points: points})
	}
	return series
}
