package llm

import (
	"net/http"
	"time"

	"github.com/emrah/mf-backend/internal/common"
	"github.com/go-chi/chi/v5"
)

// Routes mounts the LLM endpoints. /models is public; everything else requires
// a valid access token (a user only ever sees their own runs).
//
// Two bounds, because these routes are not alike. Everything here is a database
// call and belongs under the short default; generation waits on a GPU on the
// other side of a tunnel and needs tens of seconds.
//
// The caller must not wrap this router in a shorter timeout. A child context
// cannot extend a parent's deadline, so an outer bound silently wins and cuts
// generation off mid-flight — which is exactly what a root-level 5s timeout did
// before these were applied here.
func (h *Handler) Routes(
	verify common.TokenVerifier,
	defaultTimeout, genTimeout time.Duration,
) http.Handler {
	r := chi.NewRouter()

	r.With(common.Timeout(defaultTimeout)).Get("/models", h.Models)

	r.Group(func(pr chi.Router) {
		pr.Use(common.RequireAuth(verify))

		pr.Group(func(sr chi.Router) {
			sr.Use(common.Timeout(defaultTimeout))
			sr.Post("/runs", h.CreateRun)
			sr.Get("/runs", h.ListRuns)
			sr.Get("/runs/{id}", h.GetRun)
			sr.Delete("/runs/{id}", h.DeleteRun)
			sr.Post("/runs/{id}/score", h.ScoreRun)
			sr.Get("/runs/{id}/score", h.GetScore)
			sr.Get("/metrics", h.Metrics)
		})

		// The only route that waits on the GPU.
		pr.With(common.Timeout(genTimeout)).Post("/generate", h.GenerateRun)
	})

	return r
}
