package analysis

import (
	"net/http"
	"time"

	"github.com/emrah/mf-backend/internal/common"
	"github.com/go-chi/chi/v5"
)

// Routes mounts the analysis endpoints.
//
// Three bounds, not one, because these routes wait on very different things.
// The catalogue and the report reads are database calls and belong under the
// short default. A single analysis waits on a GPU across a tunnel. A trial
// waits on that same GPU N times in sequence, so its bound has to be a multiple
// of the generation bound rather than the same number — a trial of five runs
// under a one-run timeout would be cut off after the first.
//
// The caller must not wrap this router in a shorter timeout: a child context
// cannot extend a parent's deadline, so an outer bound silently wins. The LLM
// routes learned this the hard way; see the note on llm.Handler.Routes.
func (h *Handler) Routes(
	verify common.TokenVerifier,
	defaultTimeout, genTimeout time.Duration,
) http.Handler {
	r := chi.NewRouter()

	r.Group(func(pr chi.Router) {
		pr.Use(common.RequireAuth(verify))

		pr.Group(func(sr chi.Router) {
			sr.Use(common.Timeout(defaultTimeout))
			sr.Get("/domains", h.Domains)
			sr.Get("/domains/{slug}/prompt", h.Prompt)
			sr.Get("/", h.List)
			sr.Get("/trials/{group}", h.TrialGroup)
		})

		pr.With(common.Timeout(genTimeout)).Post("/run", h.Analyze)

		// maxTrials generations back to back, plus room for the database work
		// between them. Bounded rather than unbounded so a stuck inference host
		// cannot pin a connection for the rest of the process's life.
		pr.With(common.Timeout(TrialTimeout(genTimeout))).Post("/trial", h.Trial)

		// Registered last: a literal path must be matched before the wildcard,
		// or "/trials/{group}" would be swallowed by "/{id}".
		pr.With(common.Timeout(defaultTimeout)).Get("/{id}", h.Get)
	})

	return r
}

// TrialTimeout scales the single-generation bound to cover a full trial run.
//
// Exported because cmd/server has to know it. http.Server.WriteTimeout applies
// to the whole exchange, so a value below this cuts the connection while the
// handler is still working — the caller then sees an empty reply with no error
// logged anywhere, because on the server's side nothing failed. Three trial
// runs were lost to exactly that before the two numbers were reconciled.
func TrialTimeout(gen time.Duration) time.Duration {
	// A little headroom over maxTrials × gen for the per-run database writes.
	// Capped so a generous LLM_TIMEOUT cannot produce an hours-long deadline —
	// and the cap matters more than it looks, because WriteTimeout is derived
	// from this, and a half-hour write bound would leave a stalled client
	// holding a connection and its database handle for a half hour.
	const ceiling = 15 * time.Minute
	d := gen*time.Duration(maxTrials) + time.Minute
	if d > ceiling {
		return ceiling
	}
	return d
}
