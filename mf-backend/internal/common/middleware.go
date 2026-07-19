package common

import (
	"context"
	"net/http"
	"strings"

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
// the whole server process.
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
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
