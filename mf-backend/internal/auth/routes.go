package auth

import (
	"errors"
	"net"
	"net/http"
	"strings"

	"github.com/emrah/mf-backend/internal/common"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Routes mounts the auth endpoints. Public routes (register/login/refresh/
// logout) are open; the rest require a valid access token.
func (h *Handler) Routes(verify common.TokenVerifier) http.Handler {
	r := chi.NewRouter()

	// Public
	r.Post("/register", h.Register)
	r.Post("/login", h.Login)
	r.Post("/refresh", h.Refresh)
	r.Post("/logout", h.Logout)

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

// clientIP extracts a best-effort client IP for session records.
func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		return strings.TrimSpace(strings.Split(fwd, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
