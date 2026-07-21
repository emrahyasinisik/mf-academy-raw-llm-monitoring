package auth

import (
	"errors"
	"net/http"
	"strings"

	"github.com/emrah/mf-backend/internal/common"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Routes mounts the auth endpoints. Public routes (register/login/refresh/
// logout) are open; the rest require a valid access token. The sensitive
// public routes are wrapped with rateLimit (per-IP) to blunt brute force and
// abuse; pass nil to disable (e.g. in tests).
func (h *Handler) Routes(verify common.TokenVerifier, rateLimit func(http.Handler) http.Handler) http.Handler {
	r := chi.NewRouter()

	// Public but sensitive — rate limited per client IP. Logout stays
	// unauthenticated by design (a client whose access token already expired
	// must still be able to retire its refresh token), but unauthenticated is
	// not a reason to leave it unmetered: every call costs a session lookup.
	r.Group(func(pr chi.Router) {
		if rateLimit != nil {
			pr.Use(rateLimit)
		}
		pr.Post("/register", h.Register)
		pr.Post("/login", h.Login)
		pr.Post("/refresh", h.Refresh)
		pr.Post("/logout", h.Logout)
	})

	// Protected
	r.Group(func(pr chi.Router) {
		pr.Use(common.RequireAuth(verify))
		pr.Get("/me", h.Me)
		pr.Patch("/me", h.UpdateMe)
		pr.Post("/change-password", h.ChangePassword)
		pr.Get("/sessions", h.ListSessions)
		pr.Delete("/sessions/{id}", h.RevokeSession)
	})

	return r
}

// ---- small helpers kept next to where they are used ----

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// validateCredentials enforces minimal input rules at the edge (staff-engineer:
// "validation at the edge").
func validateCredentials(email, password string) error {
	if !strings.Contains(email, "@") || len(email) < 3 {
		return common.ErrBadRequest("a valid email is required")
	}
	if len(password) < 8 {
		return common.ErrBadRequest("password must be at least 8 characters")
	}
	return nil
}

// isUniqueViolation detects Postgres error 23505 (unique_violation).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}

func chiURLParam(r *http.Request, key string) string {
	return chi.URLParam(r, key)
}
