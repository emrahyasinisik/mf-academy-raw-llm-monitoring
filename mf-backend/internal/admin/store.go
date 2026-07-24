package admin

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNoRows means the addressed adapter does not exist.
var ErrNoRows = errors.New("no rows")

// ErrBadTransition means the requested status is not reachable from the current
// one. Carried as a distinct error so the handler can answer 409 rather than
// 400: the request was well-formed, it just arrived against the wrong state.
var ErrBadTransition = errors.New("invalid status transition")

// Store owns the SQL for adapters and the operator's read-only views.
type Store struct {
	db *pgxpool.Pool
}

func NewStore(db *pgxpool.Pool) *Store { return &Store{db: db} }

const adapterColumns = `
	id, name, base_model, status, lora_rank, lora_alpha, target_modules,
	mlc_model_id, metrics, notes, last_error, created_by, created_at,
	updated_at, activated_at`

func scanAdapter(row pgx.Row) (Adapter, error) {
	var a Adapter
	var metrics []byte
	err := row.Scan(&a.ID, &a.Name, &a.BaseModel, &a.Status, &a.LoRARank, &a.LoRAAlpha,
		&a.TargetModules, &a.MLCModelID, &metrics, &a.Notes, &a.LastError,
		&a.CreatedBy, &a.CreatedAt, &a.UpdatedAt, &a.ActivatedAt)
	if err != nil {
		return Adapter{}, err
	}
	if len(metrics) == 0 {
		metrics = []byte("{}")
	}
	a.Metrics = metrics
	return a, nil
}

// CreateAdapter registers a build.
func (s *Store) CreateAdapter(ctx context.Context, userID string, req CreateAdapterRequest) (Adapter, error) {
	return scanAdapter(s.db.QueryRow(ctx,
		`INSERT INTO llm_adapters (name, base_model, lora_rank, lora_alpha, target_modules, notes, created_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)
		 RETURNING`+adapterColumns,
		req.Name, req.BaseModel, req.LoRARank, req.LoRAAlpha, req.TargetModules, req.Notes, userID))
}

// ListAdapters returns every build, newest first.
//
// Unpaginated on purpose. Each row is one training run on one 6 GB card that
// takes tens of minutes; a deployment that accumulates enough of them to need
// paging is years away, and a cursor here would be machinery guarding against
// nothing.
func (s *Store) ListAdapters(ctx context.Context) ([]Adapter, error) {
	rows, err := s.db.Query(ctx, `SELECT`+adapterColumns+` FROM llm_adapters ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Adapter{}
	for rows.Next() {
		a, err := scanAdapter(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// GetAdapter returns one build.
func (s *Store) GetAdapter(ctx context.Context, id string) (Adapter, error) {
	a, err := scanAdapter(s.db.QueryRow(ctx, `SELECT`+adapterColumns+` FROM llm_adapters WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Adapter{}, ErrNoRows
	}
	return a, err
}

// UpdateStatus advances a build, enforcing the state machine.
//
// The read and the write share one transaction with the row locked. Two
// concurrent callbacks from a retrying pipeline would otherwise both read
// `merging`, both find their transition legal, and both write — leaving the
// final status decided by whichever connection the pool happened to schedule
// last. SELECT ... FOR UPDATE makes the second one wait and re-validate.
func (s *Store) UpdateStatus(ctx context.Context, id string, req UpdateStatusRequest) (Adapter, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Adapter{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var current string
	err = tx.QueryRow(ctx, `SELECT status FROM llm_adapters WHERE id = $1 FOR UPDATE`, id).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		return Adapter{}, ErrNoRows
	}
	if err != nil {
		return Adapter{}, err
	}
	if !ValidTransition(current, req.Status) {
		return Adapter{}, ErrBadTransition
	}

	// metrics is merged rather than replaced: the pipeline reports different
	// keys at different stages (training reports loss, compiling reports wall
	// clock), and a stage that overwrote the whole object would discard what
	// the previous one measured. `||` is jsonb's shallow merge, right-biased.
	var metrics []byte
	if len(req.Metrics) > 0 {
		metrics = req.Metrics
	}

	a, err := scanAdapter(tx.QueryRow(ctx,
		`UPDATE llm_adapters SET
		     status       = $2,
		     mlc_model_id = CASE WHEN $3::text <> '' THEN $3 ELSE mlc_model_id END,
		     metrics      = CASE WHEN $4::jsonb IS NOT NULL THEN metrics || $4::jsonb ELSE metrics END,
		     last_error   = CASE WHEN $2 = 'failed' THEN $5 ELSE '' END,
		     updated_at   = now()
		 WHERE id = $1
		 RETURNING`+adapterColumns,
		id, req.Status, req.MLCModelID, metrics, req.Error))
	if err != nil {
		return Adapter{}, err
	}
	return a, tx.Commit(ctx)
}

// MarkActive flips the status columns so exactly one row reads `active`.
//
// Two statements in one transaction: demote whoever is active, promote the new
// one. The demotion runs first and unconditionally, so the invariant holds even
// if a previous crash left two rows active. Which adapter *generation* actually
// uses is settings.active_adapter_id — this column is the panel's display state
// and is kept consistent with it by the handler that calls both.
func (s *Store) MarkActive(ctx context.Context, id string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`UPDATE llm_adapters SET status = 'ready', updated_at = now() WHERE status = 'active'`); err != nil {
		return err
	}
	if id != "" {
		tag, err := tx.Exec(ctx,
			`UPDATE llm_adapters SET status = 'active', activated_at = now(), updated_at = now()
			  WHERE id = $1 AND status IN ('ready', 'active')`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNoRows
		}
	}
	return tx.Commit(ctx)
}

// DeleteAdapter removes a build. The settings row's FK is ON DELETE SET NULL,
// so deleting the active adapter falls back to the base model rather than
// failing — the operator gets a working system, not a locked one.
func (s *Store) DeleteAdapter(ctx context.Context, id string) error {
	tag, err := s.db.Exec(ctx, `DELETE FROM llm_adapters WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNoRows
	}
	return nil
}

// Logs returns recent runs across every user, newest first, by cursor.
//
// The LEFT JOIN on scores mirrors the runs list: a run that has not been scored
// is still a run worth seeing in the monitor, so it must not be filtered out by
// an inner join.
func (s *Store) Logs(ctx context.Context, limit int, before time.Time, target string) (LogPage, error) {
	// One row over the asked-for limit tells us whether another page exists
	// without a second COUNT query.
	rows, err := s.db.Query(ctx,
		`SELECT r.id, u.email, r.model, r.target, r.prompt_tokens, r.completion_tokens,
		        r.latency_ms, sc.score, coalesce(sc.grade, ''), r.created_at
		   FROM llm_runs r
		   JOIN users u ON u.id = r.user_id
		   LEFT JOIN llm_scores sc ON sc.run_id = r.id
		  WHERE ($1::timestamptz IS NULL OR r.created_at < $1)
		    AND ($2::text = '' OR r.target = $2)
		  ORDER BY r.created_at DESC
		  LIMIT $3`,
		nullableTime(before), target, limit+1)
	if err != nil {
		return LogPage{}, err
	}
	defer rows.Close()

	page := LogPage{Entries: []LogEntry{}, Limit: limit}
	for rows.Next() {
		var e LogEntry
		if err := rows.Scan(&e.ID, &e.UserEmail, &e.Model, &e.Target, &e.PromptTokens,
			&e.CompletionTokens, &e.LatencyMs, &e.Score, &e.Grade, &e.CreatedAt); err != nil {
			return LogPage{}, err
		}
		page.Entries = append(page.Entries, e)
	}
	if err := rows.Err(); err != nil {
		return LogPage{}, err
	}

	if len(page.Entries) > limit {
		page.Entries = page.Entries[:limit]
		page.HasMore = true
		cursor := page.Entries[len(page.Entries)-1].CreatedAt
		page.NextCursor = &cursor
	}
	return page, nil
}

// Overview computes the panel header in one round trip.
//
// One statement of scalar subqueries rather than six queries: each subquery is
// independent, the planner runs them in a single pass, and the panel's first
// paint stops costing six pool checkouts. The 24-hour window is repeated rather
// than factored into a CTE because the planner already caches the expression
// and a CTE here would be an optimisation fence, not an optimisation.
func (s *Store) Overview(ctx context.Context) (Overview, error) {
	var o Overview
	err := s.db.QueryRow(ctx, `
		SELECT
		    (SELECT count(*) FROM users),
		    (SELECT count(*) FROM llm_runs),
		    (SELECT count(*) FROM llm_runs WHERE created_at > now() - interval '24 hours'),
		    (SELECT coalesce(avg(latency_ms), 0) FROM llm_runs WHERE created_at > now() - interval '24 hours'),
		    (SELECT coalesce(percentile_cont(0.95) WITHIN GROUP (ORDER BY latency_ms), 0)
		       FROM llm_runs WHERE created_at > now() - interval '24 hours'),
		    (SELECT count(*) FROM llm_adapters),
		    (SELECT count(*) FROM llm_adapters WHERE status IN ('ready', 'active')),
		    (SELECT count(*) FROM assessments),
		    (SELECT count(*) FROM assessments WHERE created_at > now() - interval '24 hours'),
		    -- avg over a boolean cast to int is the share that were valid.
		    -- coalesce covers the no-assessments-yet case, where avg is NULL
		    -- and would otherwise scan into a non-pointer float and fail.
		    (SELECT coalesce(avg(schema_valid::int), 0) FROM assessments
		      WHERE created_at > now() - interval '24 hours'),
		    (SELECT active_adapter_id FROM llm_settings WHERE id = 1)`,
	).Scan(&o.TotalUsers, &o.TotalRuns, &o.RunsLast24h, &o.AvgLatencyMs, &o.P95LatencyMs,
		&o.AdaptersTotal, &o.AdaptersReady, &o.Assessments, &o.AssessmentsLast24h,
		&o.SchemaValidRate24h, &o.ActiveAdapterID)
	return o, err
}

// nullableTime maps Go's zero time onto SQL NULL, which is what the "no cursor,
// give me the newest page" case means.
func nullableTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}

// uniqueViolation is PostgreSQL's SQLSTATE for a duplicate key.
const uniqueViolation = "23505"

// isUniqueViolation distinguishes "you picked a name that is taken" from a real
// failure. Checked against the driver's structured error code rather than the
// message text, which is localised by the server's lc_messages and would stop
// matching the moment the database ran under a different locale.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolation
}
