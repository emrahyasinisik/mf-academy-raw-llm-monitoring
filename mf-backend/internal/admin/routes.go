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
// Almost everything here is a database call, so one short bound covers it —
// the exception is /metrics, which reads Prometheus across the tunnel and is
// given its own group below.
//
// The two bounds are siblings rather than one nested in the other, and that is
// the whole point: a child context cannot extend its parent's deadline, so a
// generous timeout written inside a five-second subtree is decoration. This
// codebase learned that on /llm/generate, where a 25-second route bound never
// once took effect and every request died at 5001ms instead. Applying the
// short bound to a group that excludes the slow route is the only arrangement
// where both numbers mean something.
func (h *Handler) Routes(verify common.TokenVerifier, timeout, remoteTimeout time.Duration) http.Handler {
	r := chi.NewRouter()

	r.Use(common.RequireAuth(verify))
	r.Use(common.RequireRole(common.RoleAdmin))

	// The one endpoint here that leaves the machine. Prometheus runs beside the
	// GPU, so this waits on a round trip through Cloudflare to a home
	// connection — an order of magnitude more than a query against the pool
	// next door, and four of them at once. The client behind it keeps a shorter
	// bound of its own so a switched-off box is still reported as an
	// unreachable store rather than as a request that ran out of time.
	r.Group(func(gr chi.Router) {
		gr.Use(common.Timeout(remoteTimeout))
		gr.Get("/metrics", h.Metrics)
	})

	r.Group(func(gr chi.Router) {
		gr.Use(common.Timeout(timeout))
		h.localRoutes(gr)
	})

	return r
}

// localRoutes are the endpoints that go no further than the database.
func (h *Handler) localRoutes(r chi.Router) {
	r.Get("/overview", h.Overview)
	r.Get("/stats", h.Stats)
	r.Get("/logs", h.Logs)

	r.Get("/settings", h.Settings)
	r.Patch("/settings", h.UpdateSettings)

	// What can be served, built-in models and adapter builds merged into one
	// list. The distinction matters to the operator; the choice does not.
	r.Get("/models", h.Models)

	r.Route("/accounts", func(ar chi.Router) {
		ar.Get("/", h.ListAccounts)
		ar.Post("/", h.CreateAccount)
		ar.Get("/{id:[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}}", h.GetAccount)
		ar.Delete("/{id:[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}}", h.DeleteAccount)
		ar.Post("/{id:[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}}/suspend", h.SuspendAccount)
		ar.Post("/{id:[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}}/unsuspend", h.UnsuspendAccount)
	})

	r.Get("/audit", h.ListAudit)

	r.Route("/legal", func(lr chi.Router) {
		lr.Get("/", h.ListLegal)
		lr.Get("/{slug}", h.GetLegal)
		lr.Put("/{slug}", h.SaveLegalDraft)
		lr.Post("/{slug}/publish", h.PublishLegal)
		lr.Delete("/{slug}/draft", h.DeleteLegalDraft)
	})

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
}
