package org

import "time"

// OrgSummary is the company-panel view of one organization. Path/body never
// carry an org id — the handler scopes every read to claims.OrgID.
type OrgSummary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	SeatLimit   int    `json:"seat_limit"`
	Status      string `json:"status"`
	MemberCount int    `json:"member_count"`
}

// MeResponse is GET /org/me: the actor's org plus their role in it.
type MeResponse struct {
	Org  OrgSummary `json:"org"`
	Role string     `json:"role"`
}

// Member is one seat in the actor's organization.
type Member struct {
	ID             string     `json:"id"`
	Email          string     `json:"email"`
	Name           string     `json:"name"`
	OrgRole        string     `json:"org_role"`
	CreatedAt      time.Time  `json:"created_at"`
	LastActivityAt *time.Time `json:"last_activity_at,omitempty"`
}

// ListMembersResponse is GET /org/members.
type ListMembersResponse struct {
	Members []Member `json:"members"`
}

// CreateMemberRequest is POST /org/members. org_role is admin|member only —
// owner cannot be minted from the company panel.
type CreateMemberRequest struct {
	Name    string `json:"name"`
	Email   string `json:"email"`
	OrgRole string `json:"org_role"`
}

// CreateMemberResponse returns the new row and the one-time plaintext password.
// The hash is what lands in the database; the plaintext is never stored.
type CreateMemberResponse struct {
	Member            Member `json:"member"`
	TemporaryPassword string `json:"temporary_password"`
}

// SetMemberRoleRequest is PATCH /org/members/{id}. admin|member only —
// owner cannot be written, and the target owner row is refused separately.
type SetMemberRoleRequest struct {
	OrgRole string `json:"org_role"`
}

// ---- Usage / stats (Postgres only; never Prometheus) ----

// StatBox is a current-vs-previous figure with an optional percent change.
// Previous == 0 leaves ChangePct nil so we do not invent a growth rate.
type StatBox struct {
	Value     float64  `json:"value"`
	Previous  float64  `json:"previous"`
	ChangePct *float64 `json:"change_pct"`
}

// MemberSeatBox is the live seat fill — not a windowed series.
type MemberSeatBox struct {
	Count     int `json:"count"`
	SeatLimit int `json:"seat_limit"`
}

// SchemaBox is the window's schema-compliance rate only. No adapter name:
// company admins do not roll adapters, and attributing the rate to one would
// mislabel a multi-adapter window the same way the admin panel once did.
type SchemaBox struct {
	Rate float64 `json:"rate"` // 0..1
}

// OrgStatsBoxes is the summary strip on /sirket and /sirket/kullanim.
type OrgStatsBoxes struct {
	Members        MemberSeatBox `json:"members"`
	ReportsLast24h StatBox       `json:"reports_last_24h"`
	ReportsWindow  StatBox       `json:"reports_window"`
	SchemaValidity SchemaBox     `json:"schema_validity"`
}

// DayPoint is one UTC day bucket for a single series (count or valid count).
type DayPoint struct {
	T int64 `json:"t"` // UTC day start, unix seconds
	V int   `json:"v"`
}

// SeriesPoint is one day value in a named target series.
type SeriesPoint struct {
	T int64   `json:"t"`
	V float64 `json:"v"`
}

// TargetSeries is llm_runs volume broken by browser|server.
type TargetSeries struct {
	Target string        `json:"target"`
	Points []SeriesPoint `json:"points"`
}

// MemberAct is per-seat analysis volume in the window — id, name, count,
// last_at. No case text, scores, or findings.
type MemberAct struct {
	UserID string     `json:"user_id"`
	Name   string     `json:"name"`
	Count  int        `json:"count"`
	LastAt *time.Time `json:"last_at,omitempty"`
}

// OrgStats is GET /org/stats — every series scoped to claims.OrgID members.
type OrgStats struct {
	Window            string         `json:"window"` // "30d" | "90d"
	From              time.Time      `json:"from"`
	To                time.Time      `json:"to"`
	Boxes             OrgStatsBoxes  `json:"boxes"`
	AssessmentsPerDay []DayPoint     `json:"assessments_per_day"`
	SchemaValidPerDay []DayPoint     `json:"schema_valid_per_day"`
	RunsByTarget      []TargetSeries `json:"runs_by_target"`
	MemberActivity    []MemberAct    `json:"member_activity"`
}

// ---- Activity feed (metadata only) ----

// Activity kinds on the company feed. Case titles, prompts, findings and
// transcripts never appear — the org admin must not open another member's
// report from this surface.
const (
	ActivityMemberJoined          = "member.joined"
	ActivityAnalysisCompleted     = "analysis.completed"
	ActivityAnalysisSchemaInvalid = "analysis.schema_invalid"
	ActivitySessionLogin          = "session.login"
)

// ActivityItem is one metadata event in GET /org/activity.
type ActivityItem struct {
	ID        string         `json:"id"`
	Kind      string         `json:"kind"`
	At        time.Time      `json:"at"`
	ActorName string         `json:"actor_name,omitempty"`
	Meta      map[string]any `json:"meta,omitempty"` // counts/flags only
}

// ActivityResponse is GET /org/activity.
type ActivityResponse struct {
	Items []ActivityItem `json:"items"`
}
