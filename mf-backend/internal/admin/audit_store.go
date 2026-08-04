package admin

import (
	"context"
	"encoding/json"
	"time"
)

// AuditEntry is one row of audit_log. detail must never hold PII or case text.
type AuditEntry struct {
	ID        string          `json:"id"`
	ActorID   *string         `json:"actor_id,omitempty"`
	Action    string          `json:"action"`
	Target    string          `json:"target"`
	Detail    json.RawMessage `json:"detail"`
	CreatedAt time.Time       `json:"created_at"`
}

// AuditListResult is a page of audit entries.
type AuditListResult struct {
	Entries []AuditEntry `json:"entries"`
	Total   int          `json:"total"`
	Page    int          `json:"page"`
	Limit   int          `json:"limit"`
}

// WriteAudit appends one audit row. Handlers log failures and continue so an
// audit outage does not block the operator action.
func (s *Store) WriteAudit(ctx context.Context, actorID, action, target string, detail map[string]any) error {
	if detail == nil {
		detail = map[string]any{}
	}
	raw, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	var actor any
	if actorID != "" {
		actor = actorID
	}
	_, err = s.db.Exec(ctx, `
		INSERT INTO audit_log (actor_id, action, target, detail)
		VALUES ($1, $2, $3, $4::jsonb)`, actor, action, target, raw)
	return err
}

// ListAudit returns newest-first audit rows.
func (s *Store) ListAudit(ctx context.Context, page, limit int) (AuditListResult, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 50
	}
	offset := (page - 1) * limit
	rows, err := s.db.Query(ctx, `
		SELECT id, actor_id, action, target, detail, created_at,
		       COUNT(*) OVER() AS total
		  FROM audit_log
		 ORDER BY created_at DESC
		 LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return AuditListResult{}, err
	}
	defer rows.Close()

	res := AuditListResult{Entries: []AuditEntry{}, Page: page, Limit: limit}
	for rows.Next() {
		var e AuditEntry
		var detail []byte
		if err := rows.Scan(&e.ID, &e.ActorID, &e.Action, &e.Target, &detail, &e.CreatedAt, &res.Total); err != nil {
			return AuditListResult{}, err
		}
		if detail == nil {
			detail = []byte("{}")
		}
		e.Detail = detail
		res.Entries = append(res.Entries, e)
	}
	return res, rows.Err()
}

// DeleteAccount removes an organization and its members. Member rows cascade
// their own content via existing ON DELETE CASCADE FKs on user-owned tables.
func (s *Store) DeleteAccount(ctx context.Context, id string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM organizations WHERE id = $1)`, id).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNoRows
	}

	// Users reference the org without ON DELETE CASCADE; delete members first.
	if _, err := tx.Exec(ctx, `DELETE FROM users WHERE org_id = $1`, id); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNoRows
	}
	return tx.Commit(ctx)
}
