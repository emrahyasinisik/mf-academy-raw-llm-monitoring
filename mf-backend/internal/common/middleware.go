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

// SecurityHeaders sets defensive response headers.
//
// The API itself returns JSON, which browsers do not render — but /docs serves
// HTML from this same origin, and anything served there runs with the API's
// origin privileges. These headers are cheap and apply to both.
//
// The CSP deliberately allows cdn.redoc.ly: the docs page loads Redoc from
// there, so a policy of 'self' alone would silently break it. That the CDN has
// to be trusted at all is itself a finding — embedding the bundle and
// tightening this to 'self' is the better end state.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		// Stop browsers second-guessing our Content-Type; a JSON response
		// sniffed as HTML is the classic route to a stored-XSS surprise.
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Content-Security-Policy",
			"default-src 'none'; "+
				"script-src 'self' https://cdn.redoc.ly 'unsafe-inline'; "+
				"style-src 'self' https://fonts.googleapis.com 'unsafe-inline'; "+
				"font-src 'self' https://fonts.gstatic.com data:; "+
				"img-src 'self' data: blob:; "+
				"connect-src 'self'; "+
				"worker-src 'self' blob:; "+
				"frame-ancestors 'none'; "+
				"base-uri 'none'; "+
				"form-action 'none'")
		next.ServeHTTP(w, r)
	})
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
			next.ServeHTTP(w, r.WithContext(ContextWithClaims(r.Context(), claims)))
		})
	}
}

// ClaimsFromContext returns the authenticated user's claims. The bool is false
// when the request did not pass through RequireAuth.
func ClaimsFromContext(ctx context.Context) (AuthClaims, bool) {
	c, ok := ctx.Value(authClaimsKey).(AuthClaims)
	return c, ok
}

// ContextWithClaims attaches claims to a context. RequireAuth uses it on the
// real path; it is exported so tests in other packages can exercise a protected
// handler directly, which is otherwise impossible with an unexported key.
func ContextWithClaims(ctx context.Context, claims AuthClaims) context.Context {
	return context.WithValue(ctx, authClaimsKey, claims)
}
