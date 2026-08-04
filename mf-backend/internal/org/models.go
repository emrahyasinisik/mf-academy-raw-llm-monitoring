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
