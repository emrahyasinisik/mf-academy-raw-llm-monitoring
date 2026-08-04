package org

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNoRows means the addressed organization does not exist.
var ErrNoRows = errors.New("no rows")

// Store owns the SQL for the company panel.
type Store struct {
	db *pgxpool.Pool
}

func NewStore(db *pgxpool.Pool) *Store { return &Store{db: db} }

// GetOrgSummary loads one organization and its current member count.
// The caller must pass claims.OrgID — never a client-supplied id.
func (s *Store) GetOrgSummary(ctx context.Context, orgID string) (OrgSummary, error) {
	var o OrgSummary
	err := s.db.QueryRow(ctx, `
		SELECT o.id, o.name, o.type, o.seat_limit, o.status,
		       count(u.id)::int AS member_count
		  FROM organizations o
		  LEFT JOIN users u ON u.org_id = o.id
		 WHERE o.id = $1
		 GROUP BY o.id`,
		orgID,
	).Scan(&o.ID, &o.Name, &o.Type, &o.SeatLimit, &o.Status, &o.MemberCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return OrgSummary{}, ErrNoRows
	}
	if err != nil {
		return OrgSummary{}, err
	}
	return o, nil
}

// ListMembers returns every user in orgID, with optional last activity from
// assessments / llm_runs. Scoped by the caller's claims.OrgID upstream.
func (s *Store) ListMembers(ctx context.Context, orgID string) ([]Member, error) {
	rows, err := s.db.Query(ctx, `
		SELECT u.id, u.email, u.name, u.org_role, u.created_at,
		       max(GREATEST(a.created_at, r.created_at)) AS last_activity_at
		  FROM users u
		  LEFT JOIN assessments a ON a.user_id = u.id
		  LEFT JOIN llm_runs r ON r.user_id = u.id
		 WHERE u.org_id = $1
		 GROUP BY u.id
		 ORDER BY u.created_at`,
		orgID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	members := []Member{}
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.ID, &m.Email, &m.Name, &m.OrgRole, &m.CreatedAt, &m.LastActivityAt); err != nil {
			return nil, err
		}
		members = append(members, m)
	}
	return members, rows.Err()
}

// CreateMember inserts a user under orgID with must_change_password=true.
// The handler already checked seat_limit; unique email is enforced by the DB.
func (s *Store) CreateMember(ctx context.Context, orgID, name, email, orgRole, passwordHash string) (Member, error) {
	var m Member
	err := s.db.QueryRow(ctx, `
		INSERT INTO users (email, password_hash, name, org_id, org_role, must_change_password)
		VALUES ($1, $2, $3, $4, $5, true)
		RETURNING id, email, name, org_role, created_at`,
		email, passwordHash, name, orgID, orgRole,
	).Scan(&m.ID, &m.Email, &m.Name, &m.OrgRole, &m.CreatedAt)
	if err != nil {
		return Member{}, err
	}
	return m, nil
}

// GetMember loads one user by id and the org they belong to. The handler
// compares org_id to claims.OrgID — a miss is 404, not 403, so probing
// another tenant's UUID reveals nothing.
func (s *Store) GetMember(ctx context.Context, userID string) (Member, string, error) {
	var m Member
	var orgID string
	err := s.db.QueryRow(ctx, `
		SELECT id, email, name, org_role, created_at, org_id
		  FROM users
		 WHERE id = $1`,
		userID,
	).Scan(&m.ID, &m.Email, &m.Name, &m.OrgRole, &m.CreatedAt, &orgID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Member{}, "", ErrNoRows
	}
	if err != nil {
		return Member{}, "", err
	}
	return m, orgID, nil
}

// SetMemberRole updates org_role. Caller already refused owner targets and
// validated admin|member.
func (s *Store) SetMemberRole(ctx context.Context, userID, orgRole string) (Member, error) {
	var m Member
	err := s.db.QueryRow(ctx, `
		UPDATE users SET org_role = $2, updated_at = now()
		 WHERE id = $1
		 RETURNING id, email, name, org_role, created_at`,
		userID, orgRole,
	).Scan(&m.ID, &m.Email, &m.Name, &m.OrgRole, &m.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Member{}, ErrNoRows
	}
	if err != nil {
		return Member{}, err
	}
	return m, nil
}

// DeleteMember hard-deletes the user row; sessions and user-scoped rows
// follow via ON DELETE CASCADE.
func (s *Store) DeleteMember(ctx context.Context, userID string) error {
	tag, err := s.db.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNoRows
	}
	return nil
}

// isUniqueViolation detects Postgres 23505 without importing admin.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
