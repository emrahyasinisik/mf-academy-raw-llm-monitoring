package org

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/emrah/mf-backend/internal/common"
)

// OrgStore is the persistence GET /org/me needs. Declared on the consuming side
// so handler tests use a fake without a live PostgreSQL.
type OrgStore interface {
	GetOrgSummary(ctx context.Context, orgID string) (OrgSummary, error)
}

// Handler serves the company-panel endpoints under /org.
type Handler struct {
	store OrgStore
}

func NewHandler(store OrgStore) *Handler {
	return &Handler{store: store}
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
