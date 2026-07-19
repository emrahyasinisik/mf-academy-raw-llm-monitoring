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

// ListSessions returns a user's sessions, newest first.
func (s *Store) ListSessions(ctx context.Context, userID string) ([]Session, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, user_agent, ip_address, expires_at, revoked_at, created_at
		 FROM sessions WHERE user_id = $1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sessions := []Session{}
	for rows.Next() {
		var se Session
		if err := rows.Scan(&se.ID, &se.UserAgent, &se.IPAddress, &se.ExpiresAt, &se.RevokedAt, &se.CreatedAt); err != nil {
			return nil, err
		}
		sessions = append(sessions, se)
	}
	return sessions, rows.Err()
}
