package org

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/emrah/mf-backend/internal/auth"
	"github.com/emrah/mf-backend/internal/common"
)

// OrgStore is the persistence the company panel needs. Declared on the
// consuming side so handler tests use a fake without a live PostgreSQL.
type OrgStore interface {
	GetOrgSummary(ctx context.Context, orgID string) (OrgSummary, error)
	ListMembers(ctx context.Context, orgID string) ([]Member, error)
	CreateMember(ctx context.Context, orgID, name, email, orgRole, passwordHash string) (Member, error)
	// GetMember returns the user and their org_id so handlers can enforce
	// claims.OrgID and re-read org_role before mutate (stale JWT / race).
	GetMember(ctx context.Context, userID string) (Member, string, error)
	SetMemberRole(ctx context.Context, userID, orgRole string) (Member, error)
	DeleteMember(ctx context.Context, userID string) error
}

// Handler serves the company-panel endpoints under /org.
type Handler struct {
	store      OrgStore
	audit      AuditWriter
	bcryptCost int
}

func NewHandler(store OrgStore, audit AuditWriter, bcryptCost int) *Handler {
	if bcryptCost < auth.MinHashCost {
		bcryptCost = auth.MinHashCost
	}
	return &Handler{store: store, audit: audit, bcryptCost: bcryptCost}
}

// Me returns the authenticated company admin's own org summary and role.
// Scope is claims.OrgID only — no path or query org id is accepted.
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	claims, ok := common.ClaimsFromContext(r.Context())
	if !ok {
		common.Error(w, common.ErrUnauthorized("authentication required"))
		return
	}

	summary, err := h.store.GetOrgSummary(r.Context(), claims.OrgID)
	if errors.Is(err, ErrNoRows) {
		common.Error(w, common.ErrNotFound("organization not found"))
		return
	}
	if err != nil {
		slog.Error("org me failed", "error", err, "org_id", claims.OrgID)
		common.Error(w, common.ErrInternal("could not load organization"))
		return
	}

	common.JSON(w, http.StatusOK, MeResponse{
		Org:  summary,
		Role: claims.OrgRole,
	})
}
