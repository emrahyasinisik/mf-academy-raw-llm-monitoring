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
// genTimeout bounds the one route that waits on an external GPU. It is applied
// here rather than by raising the global REQUEST_TIMEOUT, because that bound is
// what protects every other endpoint from a slow query holding a connection:
// stretching it to suit the slowest handler would remove the protection from
// the twenty that do not need it.
func (h *Handler) Routes(verify common.TokenVerifier, genTimeout time.Duration) http.Handler {
	r := chi.NewRouter()

	r.Get("/models", h.Models)

	r.Group(func(pr chi.Router) {
		pr.Use(common.RequireAuth(verify))
		pr.Post("/runs", h.CreateRun)
		pr.Get("/runs", h.ListRuns)
		pr.Get("/runs/{id}", h.GetRun)
		pr.Delete("/runs/{id}", h.DeleteRun)
		pr.Post("/runs/{id}/score", h.ScoreRun)
		pr.Get("/runs/{id}/score", h.GetScore)
		pr.Get("/metrics", h.Metrics)

		// The only route that waits on the GPU, and the only one with a bound
		// measured in tens of seconds rather than single digits.
		pr.With(common.Timeout(genTimeout)).Post("/generate", h.GenerateRun)
	})

	return r
}
