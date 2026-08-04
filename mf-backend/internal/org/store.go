package org

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
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
