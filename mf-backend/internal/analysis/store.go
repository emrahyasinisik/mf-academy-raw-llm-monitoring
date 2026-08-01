package analysis

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNoRows means the addressed domain or assessment does not exist.
var ErrNoRows = errors.New("no rows")

// Store owns the SQL for rubrics and reports.
type Store struct {
	db *pgxpool.Pool
}

func NewStore(db *pgxpool.Pool) *Store { return &Store{db: db} }

const domainColumns = `
	id, slug, name, description, version, criteria, guidance,
	adapter_id, is_active, created_at, updated_at`

func scanDomain(row pgx.Row) (Domain, error) {
	var d Domain
	var criteria []byte
	err := row.Scan(&d.ID, &d.Slug, &d.Name, &d.Description, &d.Version, &criteria,
		&d.Guidance, &d.AdapterID, &d.IsActive, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return Domain{}, err
	}
	// A rubric that will not decode is unusable — every downstream number is
	// computed from it — so this is an error rather than an empty slice. The
	// alternative would produce reports scored against nothing.
	if err := json.Unmarshal(criteria, &d.Criteria); err != nil {
		return Domain{}, err
	}
	return d, nil
}

// GetDomain returns one rubric by slug.
func (s *Store) GetDomain(ctx context.Context, slug string) (Domain, error) {
	d, err := scanDomain(s.db.QueryRow(ctx,
		`SELECT`+domainColumns+` FROM analysis_domains WHERE slug = $1`, slug))
	if errors.Is(err, pgx.ErrNoRows) {
		return Domain{}, ErrNoRows
	}
	return d, err
}

// ListDomains returns the rubrics on offer. Inactive ones are excluded from the
// catalogue but remain readable by slug, so an old report can still resolve the
// domain it was produced under.
func (s *Store) ListDomains(ctx context.Context) ([]Domain, error) {
	rows, err := s.db.Query(ctx,
		`SELECT`+domainColumns+` FROM analysis_domains WHERE is_active ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Domain{}
	for rows.Next() {
		d, err := scanDomain(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// CreateAssessment stores a completed report.
//
// The criteria snapshot is written from the Assessment rather than read back
// from the domain row inside this statement. That is the point of the snapshot:
// if the rubric were re-read here, a weight edited between generation and save
// would be recorded as the one the report was scored under, which it was not.
func (s *Store) CreateAssessment(ctx context.Context, a Assessment) (Assessment, error) {
	criteria, err := json.Marshal(a.CriteriaSnapshot)
	if err != nil {
		return Assessment{}, err
	}
	findings, err := json.Marshal(a.Findings)
	if err != nil {
		return Assessment{}, err
	}

	err = s.db.QueryRow(ctx,
		`INSERT INTO assessments
		   (user_id, domain_id, domain_version, criteria_snapshot, subject_title, subject,
		    findings, overall_score, coverage, schema_valid, repair_attempts, trial_group,
		    model, target, adapter_id, latency_ms, prompt_tokens, completion_tokens, raw_response)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
		 RETURNING id, created_at`,
		a.UserID, a.DomainID, a.DomainVersion, criteria, a.SubjectTitle, a.Subject,
		findings, a.OverallScore, a.Coverage, a.SchemaValid, a.RepairAttempts, a.TrialGroup,
		a.Model, a.Target, a.AdapterID, a.LatencyMs, a.PromptTokens, a.CompletionTokens,
		a.RawResponse,
	).Scan(&a.ID, &a.CreatedAt)
	return a, err
}

const assessmentColumns = `
	a.id, a.user_id, a.domain_id, d.slug, d.name, a.domain_version, a.criteria_snapshot,
	a.subject_title, a.subject, a.findings, a.overall_score, a.coverage,
	a.schema_valid, a.repair_attempts, a.trial_group, a.model, a.target,
	a.adapter_id, a.latency_ms, a.prompt_tokens, a.completion_tokens,
	a.raw_response, a.created_at, a.redacted_at`

func scanAssessment(row pgx.Row) (Assessment, error) {
	var a Assessment
	var criteria, findings []byte
	err := row.Scan(&a.ID, &a.UserID, &a.DomainID, &a.DomainSlug, &a.DomainName,
		&a.DomainVersion, &criteria, &a.SubjectTitle, &a.Subject, &findings,
		&a.OverallScore, &a.Coverage, &a.SchemaValid, &a.RepairAttempts, &a.TrialGroup,
		&a.Model, &a.Target, &a.AdapterID, &a.LatencyMs, &a.PromptTokens,
		&a.CompletionTokens, &a.RawResponse, &a.CreatedAt, &a.RedactedAt)
	if err != nil {
		return Assessment{}, err
	}
	if err := json.Unmarshal(criteria, &a.CriteriaSnapshot); err != nil {
		return Assessment{}, err
	}
	if err := json.Unmarshal(findings, &a.Findings); err != nil {
		return Assessment{}, err
	}
	return a, nil
}

// GetAssessment returns one report, scoped to its owner.
func (s *Store) GetAssessment(ctx context.Context, userID, id string) (Assessment, error) {
	a, err := scanAssessment(s.db.QueryRow(ctx,
		`SELECT`+assessmentColumns+`
		   FROM assessments a JOIN analysis_domains d ON d.id = a.domain_id
		  WHERE a.id = $1 AND a.user_id = $2`, id, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Assessment{}, ErrNoRows
	}
	return a, err
}

// ListAssessments returns a page of the user's reports, newest first.
//
// Summaries only: subject, findings and raw_response are the large columns and
// none of them is rendered in a list. Fetching them would make every page pay
// for TOAST decompression of text nobody displays — the same lesson the runs
// list already learned.
func (s *Store) ListAssessments(
	ctx context.Context, userID, domainSlug string, limit int, before time.Time,
) (ListResult, error) {
	var beforeArg any
	if !before.IsZero() {
		beforeArg = before
	}

	rows, err := s.db.Query(ctx,
		`SELECT a.id, d.slug, d.name, a.subject_title, a.overall_score, a.coverage,
		        a.schema_valid, a.model, a.latency_ms, a.created_at, a.redacted_at
		   FROM assessments a JOIN analysis_domains d ON d.id = a.domain_id
		  WHERE a.user_id = $1
		    AND ($2::text = '' OR d.slug = $2)
		    AND ($3::timestamptz IS NULL OR a.created_at < $3)
		  ORDER BY a.created_at DESC
		  LIMIT $4`,
		userID, domainSlug, beforeArg, limit+1)
	if err != nil {
		return ListResult{}, err
	}
	defer rows.Close()

	out := ListResult{Assessments: []AssessmentSummary{}, Limit: limit}
	for rows.Next() {
		var s AssessmentSummary
		if err := rows.Scan(&s.ID, &s.DomainSlug, &s.DomainName, &s.SubjectTitle,
			&s.OverallScore, &s.Coverage, &s.SchemaValid, &s.Model, &s.LatencyMs,
			&s.CreatedAt, &s.RedactedAt); err != nil {
			return ListResult{}, err
		}
		out.Assessments = append(out.Assessments, s)
	}
	if err := rows.Err(); err != nil {
		return ListResult{}, err
	}

	if len(out.Assessments) > limit {
		out.Assessments = out.Assessments[:limit]
		out.HasMore = true
		cursor := out.Assessments[len(out.Assessments)-1].CreatedAt
		out.NextCursor = &cursor
	}
	return out, nil
}

// RedactAssessment blanks the personal columns of one report the caller owns.
//
// Two round trips in the miss case, and deliberately so. The UPDATE cannot tell
// "this row is not yours" from "you already did this": both change zero rows.
// Only the second is a success, and collapsing them would either 404 a
// legitimate repeat click or 204 a probe for someone else's report id.
func (s *Store) RedactAssessment(ctx context.Context, userID, id string) (bool, error) {
	tag, err := s.db.Exec(ctx,
		`UPDATE assessments
		    SET subject = '', subject_title = '', findings = '[]'::jsonb,
		        raw_response = '', redacted_at = now()
		  WHERE id = $1 AND user_id = $2 AND redacted_at IS NULL`,
		id, userID)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() > 0 {
		return true, nil
	}

	var one int
	err = s.db.QueryRow(ctx,
		`SELECT 1 FROM assessments WHERE id = $1 AND user_id = $2`, id, userID).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrNoRows
	}
	if err != nil {
		return false, err
	}
	return false, nil
}

// TrialAssessments returns every run in a consistency group, oldest first.
func (s *Store) TrialAssessments(ctx context.Context, userID, group string) ([]Assessment, error) {
	rows, err := s.db.Query(ctx,
		`SELECT`+assessmentColumns+`
		   FROM assessments a JOIN analysis_domains d ON d.id = a.domain_id
		  WHERE a.trial_group = $1 AND a.user_id = $2
		  ORDER BY a.created_at`, group, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Assessment{}
	for rows.Next() {
		a, err := scanAssessment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
