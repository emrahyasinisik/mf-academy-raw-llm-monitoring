package auth

import (
	"context"
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
	RevokeSession(ctx context.Context, sessionID string) error
	RevokeSessionForUser(ctx context.Context, sessionID, userID string) error
	ListSessions(ctx context.Context, userID string) ([]Session, error)
}

// Handler holds the dependencies the auth HTTP handlers need.
type Handler struct {
	store  UserStore
	tokens *TokenService
}

func NewHandler(store UserStore, tokens *TokenService) *Handler {
	return &Handler{store: store, tokens: tokens}
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
		hash, e = bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
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
	if err != nil {
		// Same error whether the email is unknown or the password is wrong —
		// do not leak which accounts exist.
		common.Error(w, common.ErrUnauthorized("invalid email or password"))
		return
	}
	cmpErr := withBcryptSlot(r.Context(), func() error {
		return bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password))
	})
	if cmpErr != nil {
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
	sessionID, userID, err := h.store.FindValidSessionByHash(r.Context(), hash)
	if err != nil {
		common.Error(w, common.ErrUnauthorized("invalid or expired refresh token"))
		return
	}
	// Rotation: kill the presented token before issuing a new one.
	_ = h.store.RevokeSession(r.Context(), sessionID)

	user, err := h.store.GetUserByID(r.Context(), userID)
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
	user, err := h.store.UpdateName(r.Context(), claims.UserID, strings.TrimSpace(req.Name))
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
		newHash, e = bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
		return e
	}); err != nil {
		common.Error(w, common.ErrInternal("could not hash password"))
		return
	}
	if err := h.store.UpdatePassword(r.Context(), claims.UserID, string(newHash)); err != nil {
		common.Error(w, common.ErrInternal("could not update password"))
		return
	}
	common.JSON(w, http.StatusOK, map[string]string{"status": "password_changed"})
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
