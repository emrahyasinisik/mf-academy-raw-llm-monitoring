package admin

import (
	"context"
	"errors"

	"github.com/emrah/mf-backend/internal/auth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type accountTx interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

func (s *Store) ListAccounts(ctx context.Context, q AccountListQuery) (AccountListResult, error) {
	offset := (q.Page - 1) * q.Limit
	rows, err := s.db.Query(ctx, `
		WITH account_rows AS (
			SELECT o.id, o.name, o.type, o.tax_id, o.seat_limit, o.status,
			       count(DISTINCT u.id)::int AS member_count,
			       count(DISTINCT a.id)::int AS assessment_count,
			       max(GREATEST(a.created_at, r.created_at)) AS last_activity_at,
			       o.created_at
			  FROM organizations o
			  LEFT JOIN users u ON u.org_id = o.id
			  LEFT JOIN assessments a ON a.user_id = u.id
			  LEFT JOIN llm_runs r ON r.user_id = u.id
			 WHERE ($1 = '' OR o.name ILIKE '%' || $1 || '%' OR u.email ILIKE '%' || $1 || '%')
			   AND ($2 = '' OR o.type = $2)
			   AND ($3 = '' OR o.status = $3)
			 GROUP BY o.id
		)
		SELECT id, name, type, tax_id, seat_limit, status, member_count, assessment_count,
		       last_activity_at, created_at, count(*) OVER()::int
		  FROM account_rows
		 ORDER BY created_at DESC
		 LIMIT $4 OFFSET $5`,
		q.Q, q.Type, q.Status, q.Limit, offset)
	if err != nil {
		return AccountListResult{}, err
	}
	defer rows.Close()

	res := AccountListResult{Accounts: []AccountSummary{}, Page: q.Page, Limit: q.Limit}
	for rows.Next() {
		var a AccountSummary
		if err := rows.Scan(&a.ID, &a.Name, &a.Type, &a.TaxID, &a.SeatLimit, &a.Status,
			&a.MemberCount, &a.AssessmentCount, &a.LastActivityAt, &a.CreatedAt, &res.Total); err != nil {
			return AccountListResult{}, err
		}
		res.Accounts = append(res.Accounts, a)
	}
	if err := rows.Err(); err != nil {
		return AccountListResult{}, err
	}
	return res, nil
}

func (s *Store) CreateIndividual(ctx context.Context, name, email, hash string) (AccountSummary, AccountMember, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return AccountSummary{}, AccountMember{}, err
	}
	defer tx.Rollback(ctx)
	return createAccountOwner(ctx, tx, accountTypeIndividual, name, "", 1, name, email, hash)
}

func (s *Store) CreateCompany(
	ctx context.Context,
	orgName, taxID string,
	seats int,
	ownerName, ownerEmail, hash string,
) (AccountSummary, AccountMember, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return AccountSummary{}, AccountMember{}, err
	}
	defer tx.Rollback(ctx)
	return createAccountOwner(ctx, tx, accountTypeCompany, orgName, taxID, seats, ownerName, ownerEmail, hash)
}

func createAccountOwner(
	ctx context.Context,
	tx accountTx,
	accountType, orgName, taxID string,
	seats int,
	ownerName, ownerEmail, hash string,
) (AccountSummary, AccountMember, error) {
	var account AccountSummary
	err := tx.QueryRow(ctx,
		`INSERT INTO organizations (name, type, tax_id, seat_limit)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, name, type, tax_id, seat_limit, status, created_at`,
		orgName, accountType, taxID, seats,
	).Scan(&account.ID, &account.Name, &account.Type, &account.TaxID, &account.SeatLimit, &account.Status, &account.CreatedAt)
	if err != nil {
		return AccountSummary{}, AccountMember{}, err
	}

	var owner AccountMember
	err = tx.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, name, org_id, org_role, must_change_password, terms_accepted_at, terms_version)
		 VALUES ($1, $2, $3, $4, 'owner', true, now(), $5)
		 RETURNING id, email, name, org_role, created_at`,
		ownerEmail, hash, ownerName, account.ID, auth.TermsVersion,
	).Scan(&owner.ID, &owner.Email, &owner.Name, &owner.OrgRole, &owner.CreatedAt)
	if err != nil {
		return AccountSummary{}, AccountMember{}, err
	}

	account.MemberCount = 1
	if err := tx.Commit(ctx); err != nil {
		return AccountSummary{}, AccountMember{}, err
	}
	return account, owner, nil
}

func (s *Store) GetAccount(ctx context.Context, id string) (AccountDetail, error) {
	var detail AccountDetail
	err := s.db.QueryRow(ctx, `
		SELECT o.id, o.name, o.type, o.tax_id, o.seat_limit, o.status,
		       count(DISTINCT u.id)::int AS member_count,
		       count(DISTINCT a.id)::int AS assessment_count,
		       max(GREATEST(a.created_at, r.created_at)) AS last_activity_at,
		       o.created_at
		  FROM organizations o
		  LEFT JOIN users u ON u.org_id = o.id
		  LEFT JOIN assessments a ON a.user_id = u.id
		  LEFT JOIN llm_runs r ON r.user_id = u.id
		 WHERE o.id = $1
		 GROUP BY o.id`,
		id,
	).Scan(&detail.ID, &detail.Name, &detail.Type, &detail.TaxID, &detail.SeatLimit,
		&detail.Status, &detail.MemberCount, &detail.AssessmentCount, &detail.LastActivityAt, &detail.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return AccountDetail{}, ErrNoRows
	}
	if err != nil {
		return AccountDetail{}, err
	}

	members, err := s.listMembers(ctx, id)
	if err != nil {
		return AccountDetail{}, err
	}
	sessions, err := s.listActiveAccountSessions(ctx, id)
	if err != nil {
		return AccountDetail{}, err
	}
	detail.Members = members
	detail.Sessions = sessions
	return detail, nil
}

func (s *Store) SetAccountStatus(ctx context.Context, id, status string) error {
	tag, err := s.db.Exec(ctx,
		`UPDATE organizations SET status = $2, updated_at = now() WHERE id = $1`,
		id, status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNoRows
	}
	return nil
}

func (s *Store) ListMemberIDs(ctx context.Context, orgID string) ([]string, error) {
	rows, err := s.db.Query(ctx, `SELECT id FROM users WHERE org_id = $1 ORDER BY created_at`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Store) RevokeAllSessionsForUser(ctx context.Context, userID string) (int64, error) {
	tag, err := s.db.Exec(ctx,
		`UPDATE sessions SET revoked_at = now()
		 WHERE user_id = $1 AND revoked_at IS NULL`, userID)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (s *Store) listMembers(ctx context.Context, orgID string) ([]AccountMember, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, email, name, org_role, created_at
		   FROM users
		  WHERE org_id = $1
		  ORDER BY created_at`,
		orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	members := []AccountMember{}
	for rows.Next() {
		var m AccountMember
		if err := rows.Scan(&m.ID, &m.Email, &m.Name, &m.OrgRole, &m.CreatedAt); err != nil {
			return nil, err
		}
		members = append(members, m)
	}
	return members, rows.Err()
}

func (s *Store) listActiveAccountSessions(ctx context.Context, orgID string) ([]AccountSession, error) {
	rows, err := s.db.Query(ctx,
		`SELECT s.id, s.user_agent, s.ip_address, s.created_at, s.expires_at
		   FROM sessions s
		   JOIN users u ON u.id = s.user_id
		  WHERE u.org_id = $1
		    AND s.revoked_at IS NULL
		    AND s.expires_at > now()
		  ORDER BY s.created_at DESC`,
		orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sessions := []AccountSession{}
	for rows.Next() {
		var se AccountSession
		if err := rows.Scan(&se.ID, &se.UserAgent, &se.IPAddress, &se.CreatedAt, &se.ExpiresAt); err != nil {
			return nil, err
		}
		sessions = append(sessions, se)
	}
	return sessions, rows.Err()
}
