package org

import (
	"net/http"
	"time"

	"github.com/emrah/mf-backend/internal/common"
	"github.com/go-chi/chi/v5"
)

// Routes mounts the company panel behind auth, a fresh-password check, and the
// org-admin gate — in that order. RequireOrgAdmin reads claims that RequireAuth
// puts on the context; RequirePasswordFresh keeps temporary-password accounts
// on /auth until they choose a password the admin no longer knows.
//
// Mounted on the whole subtree so a new /org endpoint inherits the gate instead
// of depending on whoever adds it remembering. This is a customer surface:
// platform admin role alone is not enough.
func (h *Handler) Routes(verify common.TokenVerifier, timeout time.Duration) http.Handler {
	r := chi.NewRouter()
	r.Use(common.RequireAuth(verify))
	r.Use(common.RequirePasswordFresh)
	r.Use(common.RequireOrgAdmin)
	r.Use(common.Timeout(timeout))
	r.Get("/me", h.Me)
	r.Get("/members", h.ListMembers)
	r.Post("/members", h.CreateMember)
	return r
}
