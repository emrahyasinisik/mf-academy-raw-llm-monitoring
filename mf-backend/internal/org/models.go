package org

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
