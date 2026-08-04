package auth

import (
	"context"
	"errors"
	"strings"
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

type authTx interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

// CreateUser inserts a user, the acceptance that created them, and their
// individual organization in one transaction.
func (s *Store) CreateUser(ctx context.Context, email, passwordHash, name, termsVersion string) (User, error) {
	return createUserWithIndividualOrg(ctx, func(ctx context.Context) (authTx, error) {
		return s.db.Begin(ctx)
	}, email, passwordHash, name, termsVersion)
}

func createUserWithIndividualOrg(
	ctx context.Context,
	begin func(context.Context) (authTx, error),
	email, passwordHash, name, termsVersion string,
) (User, error) {
	var u User

	tx, err := begin(ctx)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback(ctx)

	orgName := strings.TrimSpace(name)
	if orgName == "" {
		orgName = email
	}

	var orgID string
	if err := tx.QueryRow(ctx,
		`INSERT INTO organizations (name, type, seat_limit)
		 VALUES ($1, 'individual', 1)
		 RETURNING id`,
		orgName,
	).Scan(&orgID); err != nil {
		return User{}, err
	}

	err = tx.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, name, org_id, org_role, must_change_password, terms_accepted_at, terms_version)
		 VALUES ($1, $2, $3, $4, 'owner', false, now(), $5)
		 RETURNING id, email, name, role, must_change_password, created_at, updated_at, terms_accepted_at, terms_version`,
		email, passwordHash, name, orgID, termsVersion,
	).Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.MustChangePassword, &u.CreatedAt, &u.UpdatedAt, &u.TermsAcceptedAt, &u.TermsVersion)
	if err != nil {
		return User{}, err
	}
	u.OrgID = &orgID
	u.OrgRole = "owner"
	u.OrgType = "individual"
	u.OrgStatus = "active"
	if err := tx.Commit(ctx); err != nil {
		return User{}, err
	}
	return u, nil
}

// AcceptTerms records acceptance of the given terms version.
//
// Re-consent must move the version even when terms_accepted_at is already set —
// otherwise a requires_reconsent publish can never clear the gate. The first
// acceptance still stamps the date; later ones keep the original date and only
// refresh the version, so "when did they first accept" stays answerable.
func (s *Store) AcceptTerms(ctx context.Context, userID, version string) error {
	_, err := s.db.Exec(ctx,
		`UPDATE users SET
		     terms_accepted_at = COALESCE(terms_accepted_at, now()),
		     terms_version = $2,
		     updated_at = now()
		 WHERE id = $1`, userID, version)
	return err
}

// RequiredTermsVersion returns the latest published kosullar version. The
// consent gate compares the user's terms_version to this value on every check —
// no process-lifetime cache, because Render can run more than one instance and
// a publish on one cannot invalidate the other's memory.
func (s *Store) RequiredTermsVersion(ctx context.Context) (string, error) {
	var version string
	err := s.db.QueryRow(ctx, `
		SELECT version FROM legal_documents
		 WHERE slug = 'kosullar' AND is_draft = false
		 ORDER BY published_at DESC
		 LIMIT 1`).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return version, err
}

// GetUserByEmailWithHash returns the user plus password hash for login checks.
func (s *Store) GetUserByEmailWithHash(ctx context.Context, email string) (User, string, error) {
	var u User
	var hash string
	err := s.db.QueryRow(ctx,
		`SELECT u.id, u.email, u.name, u.role, u.must_change_password,
		        u.created_at, u.updated_at, u.terms_accepted_at, u.terms_version, u.password_hash,
		        u.org_id, COALESCE(u.org_role, ''), COALESCE(o.type, ''), COALESCE(o.status, 'active')
		 FROM users u
		 LEFT JOIN organizations o ON o.id = u.org_id
		 WHERE u.email = $1`, email,
	).Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.MustChangePassword, &u.CreatedAt, &u.UpdatedAt, &u.TermsAcceptedAt, &u.TermsVersion, &hash,
		&u.OrgID, &u.OrgRole, &u.OrgType, &u.OrgStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, "", ErrNoRows
	}
	return u, hash, err
}

// GetUserByID returns a user by id.
func (s *Store) GetUserByID(ctx context.Context, id string) (User, error) {
	var u User
	err := s.db.QueryRow(ctx,
		`SELECT u.id, u.email, u.name, u.role, u.must_change_password,
		        u.created_at, u.updated_at, u.terms_accepted_at, u.terms_version,
		        u.org_id, COALESCE(u.org_role, ''), COALESCE(o.type, ''), COALESCE(o.status, 'active')
		   FROM users u
		   LEFT JOIN organizations o ON o.id = u.org_id
		  WHERE u.id = $1`, id,
	).Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.MustChangePassword, &u.CreatedAt, &u.UpdatedAt, &u.TermsAcceptedAt, &u.TermsVersion,
		&u.OrgID, &u.OrgRole, &u.OrgType, &u.OrgStatus)
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
		`WITH updated AS (
		   UPDATE users SET name = $2, updated_at = now() WHERE id = $1
		   RETURNING id, email, name, role, must_change_password, created_at, updated_at,
		             terms_accepted_at, terms_version, org_id, org_role
		 )
		 SELECT u.id, u.email, u.name, u.role, u.must_change_password, u.created_at, u.updated_at,
		        u.terms_accepted_at, u.terms_version,
		        u.org_id, COALESCE(u.org_role, ''), COALESCE(o.type, ''), COALESCE(o.status, 'active')
		   FROM updated u
		   LEFT JOIN organizations o ON o.id = u.org_id`, id, name,
	).Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.MustChangePassword, &u.CreatedAt, &u.UpdatedAt, &u.TermsAcceptedAt, &u.TermsVersion,
		&u.OrgID, &u.OrgRole, &u.OrgType, &u.OrgStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNoRows
	}
	return u, err
}

// UpdatePassword sets a new password hash.
func (s *Store) UpdatePassword(ctx context.Context, id, newHash string) error {
	_, err := s.db.Exec(ctx,
		`UPDATE users SET password_hash = $2, must_change_password = false, updated_at = now() WHERE id = $1`, id, newHash)
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
