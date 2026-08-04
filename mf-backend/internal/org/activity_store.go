package org

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Activity returns a newest-first union of org-scoped metadata events.
// SELECT lists never touch subject, subject_title, findings, prompt, or
// raw_response — those columns are content, and the company feed must not
// carry them even by accident.
func (s *Store) Activity(ctx context.Context, orgID string, limit int, before *time.Time) ([]ActivityItem, error) {
	// before defaults to far future so the same query shape works with or
	// without a cursor — avoids concatenating SQL for a panel endpoint.
	cutoff := time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)
	if before != nil {
		cutoff = *before
	}

	rows, err := s.db.Query(ctx, `
		(
			SELECT u.id::text AS id,
			       $4::text AS kind,
			       u.created_at AS at,
			       u.name AS actor_name,
			       '{}'::jsonb AS meta
			  FROM users u
			 WHERE u.org_id = $1 AND u.created_at < $2
		)
		UNION ALL
		(
			SELECT a.id::text,
			       CASE WHEN a.schema_valid THEN $5 ELSE $6 END,
			       a.created_at,
			       u.name,
			       jsonb_build_object('schema_valid', a.schema_valid)
			  FROM assessments a
			  JOIN users u ON u.id = a.user_id
			 WHERE u.org_id = $1 AND a.created_at < $2
		)
		UNION ALL
		(
			SELECT s.id::text,
			       $7::text,
			       s.created_at,
			       u.name,
			       '{}'::jsonb
			  FROM sessions s
			  JOIN users u ON u.id = s.user_id
			 WHERE u.org_id = $1 AND s.created_at < $2
		)
		ORDER BY at DESC
		LIMIT $3
	`, orgID, cutoff, limit,
		ActivityMemberJoined,
		ActivityAnalysisCompleted,
		ActivityAnalysisSchemaInvalid,
		ActivitySessionLogin,
	)
	if err != nil {
		return nil, fmt.Errorf("read org activity: %w", err)
	}
	defer rows.Close()

	items := []ActivityItem{}
	for rows.Next() {
		var (
			item    ActivityItem
			rawMeta []byte
		)
		if err := rows.Scan(&item.ID, &item.Kind, &item.At, &item.ActorName, &rawMeta); err != nil {
			return nil, fmt.Errorf("scan org activity: %w", err)
		}
		if len(rawMeta) > 0 && string(rawMeta) != "{}" && string(rawMeta) != "null" {
			meta := map[string]any{}
			if err := json.Unmarshal(rawMeta, &meta); err != nil {
				return nil, fmt.Errorf("decode org activity meta: %w", err)
			}
			item.Meta = meta
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read org activity: %w", err)
	}
	return items, nil
}
