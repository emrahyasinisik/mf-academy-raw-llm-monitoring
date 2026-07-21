package auth

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store wraps all auth-related database access. Keeping SQL in one type makes
// handlers readable and the queries easy to find and test.
type Store struct {
	db *pgxpool.Pool
}

func NewStore(db *pgxpool.Pool) *Store { return &Store{db: db} }

// ErrNoRows is returned when a lookup finds nothing.
var ErrNoRows = errors.New("no rows")

// CreateUser inserts a user and returns the created row. A unique-violation on
// email is surfaced so the handler can return 409 Conflict.
func (s *Store) CreateUser(ctx context.Context, email, passwordHash, name string) (User, error) {
	var u User
	err := s.db.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, name)
		 VALUES ($1, $2, $3)
		 RETURNING id, email, name, role, created_at, updated_at`,
		email, passwordHash, name,
	).Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.CreatedAt, &u.UpdatedAt)
	return u, err
}

// GetUserByEmailWithHash returns the user plus password hash for login checks.
func (s *Store) GetUserByEmailWithHash(ctx context.Context, email string) (User, string, error) {
	var u User
	var hash string
	err := s.db.QueryRow(ctx,
		`SELECT id, email, name, role, created_at, updated_at, password_hash
		 FROM users WHERE email = $1`, email,
	).Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.CreatedAt, &u.UpdatedAt, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, "", ErrNoRows
	}
	return u, hash, err
}

// GetUserByID returns a user by id.
func (s *Store) GetUserByID(ctx context.Context, id string) (User, error) {
	var u User
	err := s.db.QueryRow(ctx,
		`SELECT id, email, name, role, created_at, updated_at FROM users WHERE id = $1`, id,
	).Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNoRows
	}
	return u, err
}

// GetPasswordHash fetches only the stored hash for a user (change-password).
func (s *Store) GetPasswordHash(ctx context.Context, id string) (string, error) {
	var hash string
	err := s.db.QueryRow(ctx, `SELECT password_hash FROM users WHERE id = $1`, id).Scan(&hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNoRows
	}
	return hash, err
}

// UpdateName updates a user's display name and returns the fresh row.
func (s *Store) UpdateName(ctx context.Context, id, name string) (User, error) {
	var u User
	err := s.db.QueryRow(ctx,
		`UPDATE users SET name = $2, updated_at = now() WHERE id = $1
		 RETURNING id, email, name, role, created_at, updated_at`, id, name,
	).Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNoRows
	}
	return u, err
}

// UpdatePassword sets a new password hash.
func (s *Store) UpdatePassword(ctx context.Context, id, newHash string) error {
	_, err := s.db.Exec(ctx,
		`UPDATE users SET password_hash = $2, updated_at = now() WHERE id = $1`, id, newHash)
	return err
}

// ---- Sessions (refresh tokens) ----

// CreateSession stores a refresh-token hash for a login.
func (s *Store) CreateSession(ctx context.Context, userID, tokenHash, userAgent, ip string, expires time.Time) (string, error) {
	var id string
	err := s.db.QueryRow(ctx,
		`INSERT INTO sessions (user_id, refresh_token_hash, user_agent, ip_address, expires_at)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		userID, tokenHash, userAgent, ip, expires,
	).Scan(&id)
	return id, err
}

// FindValidSessionByHash returns the owning user id for an active (non-revoked,
// non-expired) refresh-token hash.
func (s *Store) FindValidSessionByHash(ctx context.Context, tokenHash string) (sessionID, userID string, err error) {
	err = s.db.QueryRow(ctx,
		`SELECT id, user_id FROM sessions
		 WHERE refresh_token_hash = $1 AND revoked_at IS NULL AND expires_at > now()`,
		tokenHash,
	).Scan(&sessionID, &userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrNoRows
	}
	return sessionID, userID, err
}

// SessionLookup is the result of resolving a refresh-token hash, including
// tokens that have already been revoked. Refresh needs to tell "no such token"
// apart from "a token we retired earlier is being presented again" — the second
// is evidence of theft and must not look like an ordinary failure.
type SessionLookup struct {
	SessionID string
	UserID    string
	Revoked   bool
	Expired   bool
}

// FindSessionByHashAnyState resolves a refresh-token hash regardless of whether
// the session is still usable.
func (s *Store) FindSessionByHashAnyState(ctx context.Context, tokenHash string) (SessionLookup, error) {
	var out SessionLookup
	err := s.db.QueryRow(ctx,
		`SELECT id, user_id, revoked_at IS NOT NULL, expires_at <= now()
		 FROM sessions WHERE refresh_token_hash = $1`, tokenHash,
	).Scan(&out.SessionID, &out.UserID, &out.Revoked, &out.Expired)
	if errors.Is(err, pgx.ErrNoRows) {
		return SessionLookup{}, ErrNoRows
	}
	return out, err
}

// RevokeAllSessionsForUser retires every live session a user has. Used when a
// password changes and when a retired refresh token resurfaces — both cases
// where the safe assumption is that someone else holds a valid token.
func (s *Store) RevokeAllSessionsForUser(ctx context.Context, userID string) (int64, error) {
	tag, err := s.db.Exec(ctx,
		`UPDATE sessions SET revoked_at = now()
		 WHERE user_id = $1 AND revoked_at IS NULL`, userID)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// RevokeSession marks one session as revoked (logout / refresh rotation).
func (s *Store) RevokeSession(ctx context.Context, sessionID string) error {
	_, err := s.db.Exec(ctx,
		`UPDATE sessions SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`, sessionID)
	return err
}

// RevokeSessionForUser revokes a session only if it belongs to the given user.
func (s *Store) RevokeSessionForUser(ctx context.Context, sessionID, userID string) error {
	tag, err := s.db.Exec(ctx,
		`UPDATE sessions SET revoked_at = now() WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`,
		sessionID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNoRows
	}
	return nil
}

// DeleteExpiredSessions permanently removes sessions that can no longer be used:
// those past their expiry, and those revoked more than a grace window ago (kept
// briefly so "recently revoked" still lists in the UI). Returns the row count so
// the caller can log how much was reaped. Safe to run periodically.
func (s *Store) DeleteExpiredSessions(ctx context.Context) (int64, error) {
	tag, err := s.db.Exec(ctx,
		`DELETE FROM sessions
		 WHERE expires_at < now()
		    OR (revoked_at IS NOT NULL AND revoked_at < now() - interval '7 days')`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ListSessions returns a user's sessions, newest first.
func (s *Store) ListSessions(ctx context.Context, userID string) ([]Session, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, user_agent, ip_address, expires_at, revoked_at, created_at
		 FROM sessions WHERE user_id = $1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Most users have a handful of sessions; sizing for that avoids the
	// grow-and-copy cycle append would otherwise walk through.
	sessions := make([]Session, 0, 8)
	for rows.Next() {
		var se Session
		if err := rows.Scan(&se.ID, &se.UserAgent, &se.IPAddress, &se.ExpiresAt, &se.RevokedAt, &se.CreatedAt); err != nil {
			return nil, err
		}
		sessions = append(sessions, se)
	}
	return sessions, rows.Err()
}
