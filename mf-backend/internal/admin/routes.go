package admin

import (
	"net/http"
	"time"

	"github.com/emrah/mf-backend/internal/common"
	"github.com/go-chi/chi/v5"
)

// Routes mounts the control plane behind an authentication check followed by a
// role check.
//
// Both, in that order, and the order is load-bearing: RequireRole reads claims
// that RequireAuth puts on the context, and treats their absence as a denial.
// Mounted on the whole subtree rather than per-route so a new endpoint added
// here inherits the gate instead of depending on whoever adds it remembering —
// the failure mode of the alternative is an admin endpoint quietly serving the
// public, which is not a mistake worth leaving available.
//
// Everything here is a database call, so one short bound covers all of it.
// Nothing in this package waits on the GPU: the build pipeline runs on the
// inference host and reports back, it is not driven synchronously from here.
func (h *Handler) Routes(verify common.TokenVerifier, timeout time.Duration) http.Handler {
	r := chi.NewRouter()

	r.Use(common.RequireAuth(verify))
	r.Use(common.RequireRole(common.RoleAdmin))
	r.Use(common.Timeout(timeout))

	r.Get("/overview", h.Overview)
	r.Get("/logs", h.Logs)

	r.Get("/settings", h.Settings)
	r.Patch("/settings", h.UpdateSettings)

	// What can be served, built-in models and adapter builds merged into one
	// list. The distinction matters to the operator; the choice does not.
	r.Get("/models", h.Models)

	r.Route("/mcp-servers", func(mr chi.Router) {
		mr.Get("/", h.ListMCPServers)
		mr.Post("/", h.CreateMCPServer)
		mr.Patch("/{id}", h.UpdateMCPServer)
		mr.Delete("/{id}", h.DeleteMCPServer)
	})

	r.Route("/adapters", func(ar chi.Router) {
		ar.Get("/", h.ListAdapters)
		ar.Post("/", h.CreateAdapter)
		// Before "/{id}" so the literal path wins over the wildcard.
		ar.Post("/deactivate", h.DeactivateAdapter)
		ar.Get("/{id}", h.GetAdapter)
		ar.Delete("/{id}", h.DeleteAdapter)
		ar.Patch("/{id}/status", h.UpdateAdapterStatus)
		ar.Post("/{id}/activate", h.ActivateAdapter)
	})

	return r
}
