package llm

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNoRows = errors.New("no rows")

// Store owns all SQL for runs and scores (repository pattern, Go Day 46).
type Store struct {
	db *pgxpool.Pool
}

func NewStore(db *pgxpool.Pool) *Store { return &Store{db: db} }

// CreateRun inserts a monitoring record and returns the stored row.
func (s *Store) CreateRun(ctx context.Context, userID string, req CreateRunRequest) (Run, error) {
	metaJSON, err := marshalMeta(req.Metadata)
	if err != nil {
		return Run{}, err
	}
	keywords := req.ExpectedKeywords
	if keywords == nil {
		keywords = []string{}
	}

	var run Run
	var meta []byte
	err = s.db.QueryRow(ctx,
		`INSERT INTO llm_runs
		   (user_id, model, prompt, response, system_prompt, prompt_tokens,
		    completion_tokens, latency_ms, temperature, expected_keywords, metadata)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		 RETURNING id, user_id, model, prompt, response, system_prompt, prompt_tokens,
		           completion_tokens, latency_ms, temperature, expected_keywords, metadata, created_at`,
		userID, req.Model, req.Prompt, req.Response, req.SystemPrompt, req.PromptTokens,
		req.CompletionTokens, req.LatencyMs, req.Temperature, keywords, metaJSON,
	).Scan(&run.ID, &run.UserID, &run.Model, &run.Prompt, &run.Response, &run.SystemPrompt,
		&run.PromptTokens, &run.CompletionTokens, &run.LatencyMs, &run.Temperature,
		&run.ExpectedKeywords, &meta, &run.CreatedAt)
	if err != nil {
		return Run{}, err
	}
	run.Metadata = unmarshalMeta(meta)
	return run, nil
}

// GetRun returns one run (with its score, if any) scoped to the owner.
func (s *Store) GetRun(ctx context.Context, userID, runID string) (Run, error) {
	var run Run
	var meta []byte
	err := s.db.QueryRow(ctx,
		`SELECT id, user_id, model, prompt, response, system_prompt, prompt_tokens,
		        completion_tokens, latency_ms, temperature, expected_keywords, metadata, created_at
		 FROM llm_runs WHERE id = $1 AND user_id = $2`, runID, userID,
	).Scan(&run.ID, &run.UserID, &run.Model, &run.Prompt, &run.Response, &run.SystemPrompt,
		&run.PromptTokens, &run.CompletionTokens, &run.LatencyMs, &run.Temperature,
		&run.ExpectedKeywords, &meta, &run.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Run{}, ErrNoRows
	}
	if err != nil {
		return Run{}, err
	}
	run.Metadata = unmarshalMeta(meta)

	if score, err := s.GetScore(ctx, run.ID); err == nil {
		run.Score = &score
	}
	return run, nil
}

// ListRuns returns a page of the user's runs, newest first, each with score.
func (s *Store) ListRuns(ctx context.Context, userID, model string, limit, offset int) (ListResult, error) {
	// Total count (respecting the optional model filter).
	var total int
	countSQL := `SELECT count(*) FROM llm_runs WHERE user_id = $1 AND ($2 = '' OR model = $2)`
	if err := s.db.QueryRow(ctx, countSQL, userID, model).Scan(&total); err != nil {
		return ListResult{}, err
	}

	rows, err := s.db.Query(ctx,
		`SELECT r.id, r.user_id, r.model, r.prompt, r.response, r.system_prompt,
		        r.prompt_tokens, r.completion_tokens, r.latency_ms, r.temperature,
		        r.expected_keywords, r.metadata, r.created_at,
		        sc.id, sc.score, sc.grade, sc.breakdown, sc.rationale, sc.created_at
		 FROM llm_runs r
		 LEFT JOIN llm_scores sc ON sc.run_id = r.id
		 WHERE r.user_id = $1 AND ($2 = '' OR r.model = $2)
		 ORDER BY r.created_at DESC
		 LIMIT $3 OFFSET $4`, userID, model, limit, offset)
	if err != nil {
		return ListResult{}, err
	}
	defer rows.Close()

	runs := []Run{}
	for rows.Next() {
		var run Run
		var meta []byte
		// Nullable score columns.
		var sID, sGrade, sRationale *string
		var sScore *float64
		var sBreakdown []byte
		var sCreated *pgxTime

		if err := rows.Scan(&run.ID, &run.UserID, &run.Model, &run.Prompt, &run.Response,
			&run.SystemPrompt, &run.PromptTokens, &run.CompletionTokens, &run.LatencyMs,
			&run.Temperature, &run.ExpectedKeywords, &meta, &run.CreatedAt,
			&sID, &sScore, &sGrade, &sBreakdown, &sRationale, &sCreated); err != nil {
			return ListResult{}, err
		}
		run.Metadata = unmarshalMeta(meta)
		if sID != nil {
			run.Score = &Score{
				ID:        *sID,
				RunID:     run.ID,
				Score:     deref(sScore),
				Grade:     derefStr(sGrade),
				Breakdown: unmarshalBreakdown(sBreakdown),
				Rationale: derefStr(sRationale),
				CreatedAt: sCreated.Time(),
			}
		}
		runs = append(runs, run)
	}
	return ListResult{Runs: runs, Total: total, Limit: limit, Offset: offset}, rows.Err()
}

// DeleteRun removes a run (and its score via ON DELETE CASCADE).
func (s *Store) DeleteRun(ctx context.Context, userID, runID string) error {
	tag, err := s.db.Exec(ctx, `DELETE FROM llm_runs WHERE id = $1 AND user_id = $2`, runID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNoRows
	}
	return nil
}

// UpsertScore stores (or replaces) the decision score for a run.
func (s *Store) UpsertScore(ctx context.Context, sc Score) (Score, error) {
	breakdownJSON, err := json.Marshal(sc.Breakdown)
	if err != nil {
		return Score{}, err
	}
	var out Score
	var bd []byte
	err = s.db.QueryRow(ctx,
		`INSERT INTO llm_scores (run_id, score, grade, breakdown, rationale)
		 VALUES ($1,$2,$3,$4,$5)
		 ON CONFLICT (run_id) DO UPDATE
		   SET score = EXCLUDED.score, grade = EXCLUDED.grade,
		       breakdown = EXCLUDED.breakdown, rationale = EXCLUDED.rationale,
		       created_at = now()
		 RETURNING id, run_id, score, grade, breakdown, rationale, created_at`,
		sc.RunID, sc.Score, sc.Grade, breakdownJSON, sc.Rationale,
	).Scan(&out.ID, &out.RunID, &out.Score, &out.Grade, &bd, &out.Rationale, &out.CreatedAt)
	if err != nil {
		return Score{}, err
	}
	out.Breakdown = unmarshalBreakdown(bd)
	return out, nil
}

// GetScore fetches the score for a run.
func (s *Store) GetScore(ctx context.Context, runID string) (Score, error) {
	var sc Score
	var bd []byte
	err := s.db.QueryRow(ctx,
		`SELECT id, run_id, score, grade, breakdown, rationale, created_at
		 FROM llm_scores WHERE run_id = $1`, runID,
	).Scan(&sc.ID, &sc.RunID, &sc.Score, &sc.Grade, &bd, &sc.Rationale, &sc.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Score{}, ErrNoRows
	}
	if err != nil {
		return Score{}, err
	}
	sc.Breakdown = unmarshalBreakdown(bd)
	return sc, nil
}

// Metrics computes the aggregate dashboard summary for a user in SQL — pushing
// aggregation to the database instead of pulling every row into Go.
func (s *Store) Metrics(ctx context.Context, userID string) (Metrics, error) {
	m := Metrics{RunsByModel: map[string]int{}, GradeDistrib: map[string]int{}}

	err := s.db.QueryRow(ctx,
		`SELECT
		    count(*),
		    coalesce(avg(latency_ms), 0),
		    coalesce(avg(completion_tokens), 0)
		 FROM llm_runs WHERE user_id = $1`, userID,
	).Scan(&m.TotalRuns, &m.AvgLatencyMs, &m.AvgCompletionTk)
	if err != nil {
		return Metrics{}, err
	}

	err = s.db.QueryRow(ctx,
		`SELECT count(*), coalesce(avg(sc.score), 0)
		 FROM llm_scores sc JOIN llm_runs r ON r.id = sc.run_id
		 WHERE r.user_id = $1`, userID,
	).Scan(&m.ScoredRuns, &m.AvgScore)
	if err != nil {
		return Metrics{}, err
	}

	if err := s.aggCount(ctx,
		`SELECT model, count(*) FROM llm_runs WHERE user_id = $1 GROUP BY model`,
		userID, m.RunsByModel); err != nil {
		return Metrics{}, err
	}
	if err := s.aggCount(ctx,
		`SELECT sc.grade, count(*) FROM llm_scores sc JOIN llm_runs r ON r.id = sc.run_id
		 WHERE r.user_id = $1 GROUP BY sc.grade`,
		userID, m.GradeDistrib); err != nil {
		return Metrics{}, err
	}
	return m, nil
}

// aggCount runs a "key, count" query into the provided map.
func (s *Store) aggCount(ctx context.Context, sql, userID string, into map[string]int) error {
	rows, err := s.db.Query(ctx, sql, userID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var n int
		if err := rows.Scan(&key, &n); err != nil {
			return err
		}
		into[key] = n
	}
	return rows.Err()
}
