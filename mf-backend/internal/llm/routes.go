package llm

import (
	"net/http"

	"github.com/emrah/mf-backend/internal/common"
	"github.com/go-chi/chi/v5"
)

// Routes mounts the LLM endpoints. /models is public; everything else requires
// a valid access token (a user only ever sees their own runs).
func (h *Handler) Routes(verify common.TokenVerifier) http.Handler {
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
	})

	return r
}
