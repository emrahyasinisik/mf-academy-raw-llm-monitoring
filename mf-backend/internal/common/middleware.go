package common

import (
	"context"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

// ctxKey is an unexported type for context keys, so no other package can
// accidentally collide with ours. This is the idiomatic Go pattern.
type ctxKey string

const authClaimsKey ctxKey = "auth_claims"

// AuthClaims is the identity extracted from a validated access token and
// stashed on the request context for handlers to read.
type AuthClaims struct {
	UserID string
	Email  string
	Role   string
}

// TokenVerifier validates a raw access token and returns its claims.
// The auth package supplies the concrete implementation; common stays
// ignorant of JWT specifics, avoiding an import cycle.
type TokenVerifier func(token string) (AuthClaims, error)

// Timeout bounds how long any single request may run, by deriving a deadline
// on its context. Because every store method threads r.Context() into pgx, the
// deadline propagates all the way down to the database driver, so a slow query
// releases its pooled connection instead of holding it until the client hangs
// up. Enforcing this centrally (rather than per-handler) means a newly added
// endpoint inherits the bound automatically instead of relying on discipline.
func Timeout(d time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// CORS returns a configured CORS middleware allowing the frontend origins.
func CORS(origins []string) func(http.Handler) http.Handler {
	return cors.Handler(cors.Options{
		AllowedOrigins:   origins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: false,
		MaxAge:           300,
	})
}

// Recover turns any panic in a handler into a clean 500 instead of crashing
// the whole server process. Crucially it also records what panicked — the
// value, the request id and a full stack trace — so a production incident is
// actually debuggable instead of silently swallowed.
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				// http.ErrAbortHandler is a sentinel meaning "stop, but don't
				// treat as an error" — re-panic so the server handles it.
				if rec == http.ErrAbortHandler {
					panic(rec)
				}
				slog.Error("panic recovered",
					"request_id", middleware.GetReqID(r.Context()),
					"method", r.Method,
					"path", r.URL.Path,
					"panic", rec,
					"stack", string(debug.Stack()),
				)
				Error(w, ErrInternal("panic recovered"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// RequireAuth is middleware that rejects requests without a valid Bearer token.
// On success it puts the AuthClaims on the request context.
func RequireAuth(verify TokenVerifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if header == "" || !strings.HasPrefix(header, "Bearer ") {
				Error(w, ErrUnauthorized("missing bearer token"))
				return
			}
			raw := strings.TrimPrefix(header, "Bearer ")
			claims, err := verify(raw)
			if err != nil {
				Error(w, ErrUnauthorized("invalid or expired token"))
				return
			}
			ctx := context.WithValue(r.Context(), authClaimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ClaimsFromContext returns the authenticated user's claims. The bool is false
// when the request did not pass through RequireAuth.
func ClaimsFromContext(ctx context.Context) (AuthClaims, bool) {
	c, ok := ctx.Value(authClaimsKey).(AuthClaims)
	return c, ok
}
