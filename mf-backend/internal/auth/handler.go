package auth

import (
	"context"
	"crypto/rand"
	"log/slog"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/emrah/mf-backend/internal/common"
	"golang.org/x/crypto/bcrypt"
)

// bcryptSem bounds how many password hashes may be computed at once.
//
// bcrypt is deliberately expensive and, being pure CPU work, never yields to
// the Go scheduler. Measured at ~50ms per verification on an M3 (and several
// times that on a shared vCPU), enough concurrent logins will occupy every P in
// the runtime — starving not just other auth requests but every unrelated
// handler, and the GC's workers with them. Capping concurrency at the core
// count keeps password hashing from crowding out the rest of the service.
var bcryptSem = make(chan struct{}, runtime.NumCPU())

// DefaultHashCost is the bcrypt work factor used unless the deployment
// overrides it. Above the library default of 10 because hardware has moved on,
// and the semaphore above keeps the extra cost from starving the scheduler.
//
// It is tunable because the right value depends on the machine: each step
// doubles the work, and measured here cost 12 takes ~200ms against ~50ms at
// cost 10. On a throttled shared vCPU that difference is the gap between a
// brisk login and a sluggish one, and a deployment should be able to make that
// trade without editing code.
const DefaultHashCost = 12

// MinHashCost is the floor. Below this bcrypt stops being meaningfully
// expensive to attack, so a misconfiguration must not be able to weaken it.
const MinHashCost = 10

// maxNameBytes caps the display name. The body limit already bounds a single
// request, but without a field-level cap a caller could still persist a
// megabyte of text per account and have it returned on every /auth/me.
const maxNameBytes = 100

// newDecoyHash builds the hash Login compares against when the submitted email
// matches no account, so a failed login costs the same bcrypt work whether or
// not the address exists. It must be generated at the same cost the handler
// uses for real passwords — a cheaper decoy would reopen the very timing gap it
// exists to close.
//
// The plaintext is random and never stored, so the comparison always fails;
// only its duration matters.
func newDecoyHash(cost int) []byte {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		panic("auth: cannot seed decoy hash: " + err.Error())
	}
	h, err := bcrypt.GenerateFromPassword(buf, cost)
	if err != nil {
		panic("auth: cannot build decoy hash: " + err.Error())
	}
	return h
}

// withBcryptSlot runs fn while holding a hashing slot. It honours the request
// context so a caller that has already timed out is rejected immediately rather
// than queueing for work whose result nobody will read.
func withBcryptSlot(ctx context.Context, fn func() error) error {
	select {
	case bcryptSem <- struct{}{}:
		defer func() { <-bcryptSem }()
		return fn()
	case <-ctx.Done():
		return ctx.Err()
	}
}

// UserStore is the persistence behaviour the auth handlers need, declared on
// the consuming side. *Store satisfies it implicitly.
type UserStore interface {
	CreateUser(ctx context.Context, email, passwordHash, name string) (User, error)
	GetUserByEmailWithHash(ctx context.Context, email string) (User, string, error)
	GetUserByID(ctx context.Context, id string) (User, error)
	GetPasswordHash(ctx context.Context, id string) (string, error)
	UpdateName(ctx context.Context, id, name string) (User, error)
	UpdatePassword(ctx context.Context, id, newHash string) error

	CreateSession(ctx context.Context, userID, tokenHash, userAgent, ip string, expires time.Time) (string, error)
	FindValidSessionByHash(ctx context.Context, tokenHash string) (sessionID, userID string, err error)
	FindSessionByHashAnyState(ctx context.Context, tokenHash string) (SessionLookup, error)
	RevokeSession(ctx context.Context, sessionID string) error
	RevokeSessionForUser(ctx context.Context, sessionID, userID string) error
	RevokeAllSessionsForUser(ctx context.Context, userID string) (int64, error)
	ListSessions(ctx context.Context, userID string) ([]Session, error)
}

// Handler holds the dependencies the auth HTTP handlers need.
type Handler struct {
	store     UserStore
	tokens    *TokenService
	hashCost  int
	decoyHash []byte
}

// NewHandler wires the auth handlers. A cost below MinHashCost is raised to it:
// the work factor is a security floor, so a bad value must fail safe rather
// than quietly weaken every password in the database.
func NewHandler(store UserStore, tokens *TokenService, hashCost int) *Handler {
	if hashCost < MinHashCost {
		slog.Warn("bcrypt cost below the permitted floor; raising it",
			"requested", hashCost, "using", MinHashCost)
		hashCost = MinHashCost
	}
	return &Handler{
		store:    store,
		tokens:   tokens,
		hashCost: hashCost,
		// Built once at startup: doing it per request would add a full bcrypt
		// round to every login that misses.
		decoyHash: newDecoyHash(hashCost),
	}
}

// Register creates a new account and immediately logs the user in.
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := common.Decode(r, &req); err != nil {
		common.Error(w, err)
		return
	}
	req.Email = normalizeEmail(req.Email)
	if err := validateCredentials(req.Email, req.Password); err != nil {
		common.Error(w, err)
		return
	}

	// bcrypt salts and hashes the password. We never store the plaintext.
	var hash []byte
	err := withBcryptSlot(r.Context(), func() error {
		var e error
		hash, e = bcrypt.GenerateFromPassword([]byte(req.Password), h.hashCost)
		return e
	})
	if err != nil {
		common.Error(w, common.ErrInternal("could not hash password"))
		return
	}

	user, err := h.store.CreateUser(r.Context(), req.Email, string(hash), strings.TrimSpace(req.Name))
	if err != nil {
		if isUniqueViolation(err) {
			common.Error(w, common.ErrConflict("email already registered"))
			return
		}
		common.Error(w, common.ErrInternal("could not create user"))
		return
	}

	h.issueTokens(w, r, user, http.StatusCreated)
}

// Login verifies credentials and returns a fresh token pair.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := common.Decode(r, &req); err != nil {
		common.Error(w, err)
		return
	}
	req.Email = normalizeEmail(req.Email)

	user, hash, err := h.store.GetUserByEmailWithHash(r.Context(), req.Email)
	found := err == nil
	if !found {
		// Compare against a decoy so an unknown address costs the same ~50ms of
		// bcrypt as a real one. Returning early here made the two cases 92x
		// apart in wall time (0.55ms vs 50.5ms), which is a single-request
		// oracle for whether an account exists — the identical error message
		// below hides nothing when the timing answers the question first.
		hash = string(h.decoyHash)
	}

	cmpErr := withBcryptSlot(r.Context(), func() error {
		return bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password))
	})
	if !found || cmpErr != nil {
		// Same error whether the email is unknown or the password is wrong —
		// do not leak which accounts exist.
		common.Error(w, common.ErrUnauthorized("invalid email or password"))
		return
	}

	h.issueTokens(w, r, user, http.StatusOK)
}

// Refresh rotates a refresh token: the old one is revoked, a new pair issued.
func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req RefreshRequest
	if err := common.Decode(r, &req); err != nil {
		common.Error(w, err)
		return
	}
	if req.RefreshToken == "" {
		common.Error(w, common.ErrBadRequest("refresh_token is required"))
		return
	}

	hash := HashToken(req.RefreshToken)
	session, err := h.store.FindSessionByHashAnyState(r.Context(), hash)
	if err != nil {
		common.Error(w, common.ErrUnauthorized("invalid or expired refresh token"))
		return
	}

	// A token we already retired is being presented again. Rotation means the
	// legitimate holder swapped it for a new one, so whoever sent this either
	// captured it or is replaying an old copy — and we cannot tell which. Since
	// one of the two parties is an attacker holding a currently-valid token,
	// the safe move is to retire the whole set and make both sign in again.
	if session.Revoked {
		n, revokeErr := h.store.RevokeAllSessionsForUser(r.Context(), session.UserID)
		slog.Warn("refresh token reuse detected; revoked all sessions",
			"user_id", session.UserID,
			"ip", common.ClientIP(r),
			"sessions_revoked", n,
			"revoke_error", revokeErr,
		)
		common.Error(w, common.ErrUnauthorized("session revoked, please sign in again"))
		return
	}
	if session.Expired {
		common.Error(w, common.ErrUnauthorized("invalid or expired refresh token"))
		return
	}

	// Rotation: kill the presented token before issuing a new one.
	_ = h.store.RevokeSession(r.Context(), session.SessionID)

	user, err := h.store.GetUserByID(r.Context(), session.UserID)
	if err != nil {
		common.Error(w, common.ErrUnauthorized("account no longer exists"))
		return
	}
	h.issueTokens(w, r, user, http.StatusOK)
}

// Logout revokes the presented refresh token.
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	var req RefreshRequest
	if err := common.Decode(r, &req); err != nil {
		common.Error(w, err)
		return
	}
	if req.RefreshToken != "" {
		if sessionID, _, err := h.store.FindValidSessionByHash(r.Context(), HashToken(req.RefreshToken)); err == nil {
			_ = h.store.RevokeSession(r.Context(), sessionID)
		}
	}
	common.JSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

// Me returns the currently authenticated user.
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	claims, _ := common.ClaimsFromContext(r.Context())
	user, err := h.store.GetUserByID(r.Context(), claims.UserID)
	if err != nil {
		common.Error(w, common.ErrNotFound("user not found"))
		return
	}
	common.JSON(w, http.StatusOK, user)
}

// UpdateMe updates the authenticated user's profile.
func (h *Handler) UpdateMe(w http.ResponseWriter, r *http.Request) {
	claims, _ := common.ClaimsFromContext(r.Context())
	var req UpdateProfileRequest
	if err := common.Decode(r, &req); err != nil {
		common.Error(w, err)
		return
	}
	name := strings.TrimSpace(req.Name)
	if len(name) > maxNameBytes {
		common.Error(w, common.ErrBadRequest("name is too long"))
		return
	}
	user, err := h.store.UpdateName(r.Context(), claims.UserID, name)
	if err != nil {
		common.Error(w, common.ErrInternal("could not update profile"))
		return
	}
	common.JSON(w, http.StatusOK, user)
}

// ChangePassword verifies the current password and sets a new one.
func (h *Handler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	claims, _ := common.ClaimsFromContext(r.Context())
	var req ChangePasswordRequest
	if err := common.Decode(r, &req); err != nil {
		common.Error(w, err)
		return
	}
	if len(req.NewPassword) < 8 {
		common.Error(w, common.ErrBadRequest("new password must be at least 8 characters"))
		return
	}
	hash, err := h.store.GetPasswordHash(r.Context(), claims.UserID)
	if err != nil {
		common.Error(w, common.ErrNotFound("user not found"))
		return
	}
	if err := withBcryptSlot(r.Context(), func() error {
		return bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.CurrentPassword))
	}); err != nil {
		common.Error(w, common.ErrUnauthorized("current password is incorrect"))
		return
	}
	var newHash []byte
	if err := withBcryptSlot(r.Context(), func() error {
		var e error
		newHash, e = bcrypt.GenerateFromPassword([]byte(req.NewPassword), h.hashCost)
		return e
	}); err != nil {
		common.Error(w, common.ErrInternal("could not hash password"))
		return
	}
	if err := h.store.UpdatePassword(r.Context(), claims.UserID, string(newHash)); err != nil {
		common.Error(w, common.ErrInternal("could not update password"))
		return
	}

	// Retire every refresh token, including this caller's. People change their
	// password precisely to eject someone who got in, and that fails if the
	// intruder's refresh token keeps minting access tokens afterwards. The
	// caller is handed a fresh pair below so they are not signed out by it.
	if n, err := h.store.RevokeAllSessionsForUser(r.Context(), claims.UserID); err != nil {
		// The password did change, so this is not a 500 — but leaving old
		// sessions alive silently would defeat the point of the operation.
		slog.Error("password changed but sessions could not be revoked",
			"user_id", claims.UserID, "error", err)
		common.Error(w, common.ErrInternal("password changed but active sessions could not be revoked; revoke them from your sessions list"))
		return
	} else if n > 0 {
		slog.Info("sessions revoked after password change", "user_id", claims.UserID, "count", n)
	}

	user, err := h.store.GetUserByID(r.Context(), claims.UserID)
	if err != nil {
		common.Error(w, common.ErrInternal("could not load user"))
		return
	}
	h.issueTokens(w, r, user, http.StatusOK)
}

// ListSessions lists the authenticated user's sessions.
func (h *Handler) ListSessions(w http.ResponseWriter, r *http.Request) {
	claims, _ := common.ClaimsFromContext(r.Context())
	sessions, err := h.store.ListSessions(r.Context(), claims.UserID)
	if err != nil {
		common.Error(w, common.ErrInternal("could not list sessions"))
		return
	}
	common.JSON(w, http.StatusOK, map[string]any{"sessions": sessions, "count": len(sessions)})
}

// RevokeSession revokes one of the authenticated user's sessions by id.
func (h *Handler) RevokeSession(w http.ResponseWriter, r *http.Request) {
	claims, _ := common.ClaimsFromContext(r.Context())
	id := chiURLParam(r, "id")
	if err := h.store.RevokeSessionForUser(r.Context(), id, claims.UserID); err != nil {
		common.Error(w, common.ErrNotFound("session not found"))
		return
	}
	common.JSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

// issueTokens creates a token pair, stores the refresh session and responds.
func (h *Handler) issueTokens(w http.ResponseWriter, r *http.Request, user User, status int) {
	access, _, err := h.tokens.GenerateAccess(user)
	if err != nil {
		common.Error(w, common.ErrInternal("could not sign token"))
		return
	}
	refresh, refreshHash, expires, err := h.tokens.GenerateRefresh()
	if err != nil {
		common.Error(w, common.ErrInternal("could not create refresh token"))
		return
	}
	if _, err := h.store.CreateSession(r.Context(), user.ID, refreshHash, r.UserAgent(), common.ClientIP(r), expires); err != nil {
		common.Error(w, common.ErrInternal("could not persist session"))
		return
	}
	common.JSON(w, status, TokenPair{
		AccessToken:  access,
		RefreshToken: refresh,
		TokenType:    "Bearer",
		ExpiresIn:    h.tokens.AccessTTLSeconds(),
		User:         user,
	})
}
