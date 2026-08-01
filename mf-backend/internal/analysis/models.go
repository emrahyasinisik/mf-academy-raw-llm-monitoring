// Package analysis is the product: a case goes in, a rubric-scored report comes
// out.
//
// The division of labour is the whole design. The model reads the case and
// fills in a rubric — one rating and its supporting quotes per criterion. This
// package does the arithmetic. No number the model writes is ever used as a
// score directly, and no weighting decision is ever delegated to it.
//
// That is not a purity argument. A report that says "68" is an opinion nobody
// can contest; a report that says "68, because `traction` rated 2 of 5 on these
// two quoted lines, at weight 0.20" is a finding an applicant can appeal and an
// operator can defend. Selling to people who must justify rejections means the
// arithmetic has to be ours, visible, and reproducible.
package analysis

import (
	"encoding/json"
	"time"
)

// Criterion is one row of a rubric.
type Criterion struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description"`
	// Guidance is written for the model, not the reader: it is what goes into
	// the prompt to say how this criterion is judged.
	Guidance string  `json:"guidance"`
	Weight   float64 `json:"weight"`
	// ScaleMax is the top of this criterion's rating scale. Per-criterion
	// rather than global so a rubric can mix a 0-5 judgement with a 0-1
	// present/absent check without needing two mechanisms.
	ScaleMax int `json:"scale_max"`
}

// EffectiveScaleMax guards against a rubric that omits the field. Zero would
// otherwise divide by zero in scoring, turning a data-entry slip into a NaN
// that propagates silently all the way to the report.
func (c Criterion) EffectiveScaleMax() int {
	if c.ScaleMax <= 0 {
		return 5
	}
	return c.ScaleMax
}

// Domain is a rubric: the criteria, their weights, and how to prompt for them.
type Domain struct {
	ID          string      `json:"id"`
	Slug        string      `json:"slug"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Version     int         `json:"version"`
	Criteria    []Criterion `json:"criteria"`
	Guidance    string      `json:"guidance"`
	AdapterID   *string     `json:"adapter_id"`
	IsActive    bool        `json:"is_active"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
}

// Finding is the model's work on one criterion.
//
// Score is a pointer and EvidenceFound is separate from it on purpose. "The
// deck never mentions the team" and "the team is weak" are different findings
// that a single numeric field cannot distinguish — collapsing them would score
// silence as failure, which is both wrong and, for anyone who has to defend the
// result, indefensible. A criterion with no evidence drops out of the score
// entirely and is reported through Coverage instead.
type Finding struct {
	Key           string   `json:"key"`
	EvidenceFound bool     `json:"evidence_found"`
	Score         *float64 `json:"score"`
	// Evidence holds verbatim quotes from the case. Quotes rather than the
	// model's paraphrase: a paraphrase cannot be checked against the source,
	// and an unverifiable citation is worse than none because it looks solid.
	Evidence  []string `json:"evidence"`
	Rationale string   `json:"rationale"`
}

// Assessment is one completed report.
type Assessment struct {
	ID     string `json:"id"`
	UserID string `json:"user_id"`

	DomainID   string `json:"domain_id"`
	DomainSlug string `json:"domain_slug,omitempty"`
	DomainName string `json:"domain_name,omitempty"`

	// The rubric as it stood when this ran. Copied, not looked up: tuning a
	// weight next month must not silently restate what last month's report
	// meant. Every number below is only interpretable against this snapshot.
	DomainVersion    int         `json:"domain_version"`
	CriteriaSnapshot []Criterion `json:"criteria_snapshot"`

	SubjectTitle string `json:"subject_title"`
	Subject      string `json:"subject,omitempty"`

	Findings []Finding `json:"findings"`

	// OverallScore is nil when nothing in the case could be assessed at all.
	// Zero would claim the case scored badly; nil says it could not be scored.
	OverallScore *float64 `json:"overall_score"`
	Coverage     float64  `json:"coverage"`

	SchemaValid    bool `json:"schema_valid"`
	RepairAttempts int  `json:"repair_attempts"`

	TrialGroup *string `json:"trial_group,omitempty"`

	Model            string  `json:"model"`
	Target           string  `json:"target"`
	AdapterID        *string `json:"adapter_id"`
	LatencyMs        int     `json:"latency_ms"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`

	// RawResponse is omitted from list views but kept on the record. It is the
	// only way to tell "the model wrote nonsense" from "our parser is wrong",
	// and once a bad report has reached a customer that distinction is the
	// entire investigation.
	RawResponse string `json:"raw_response,omitempty"`

	CreatedAt time.Time `json:"created_at"`

	// Non-nil once the personal columns have been blanked, by the owner's own
	// request or by the retention sweep. The reader needs this to tell "silindi"
	// from "boş geldi": a redacted report has an empty subject for a reason, and
	// showing it as missing data would misdescribe what happened to it.
	RedactedAt *time.Time `json:"redacted_at"`
}

// AssessmentSummary is the list projection. Subject, findings and raw response
// are the large columns and none of them is rendered in a list — the runs list
// already learned this lesson, see llm.RunSummary.
type AssessmentSummary struct {
	ID           string    `json:"id"`
	DomainSlug   string    `json:"domain_slug"`
	DomainName   string    `json:"domain_name"`
	SubjectTitle string    `json:"subject_title"`
	OverallScore *float64  `json:"overall_score"`
	Coverage     float64   `json:"coverage"`
	SchemaValid  bool      `json:"schema_valid"`
	Model        string    `json:"model"`
	LatencyMs    int       `json:"latency_ms"`
	CreatedAt    time.Time `json:"created_at"`

	// Non-nil once the personal columns have been blanked, by the owner's own
	// request or by the retention sweep. The reader needs this to tell "silindi"
	// from "boş geldi": a redacted report has an empty subject for a reason, and
	// showing it as missing data would misdescribe what happened to it.
	RedactedAt *time.Time `json:"redacted_at"`
}

// ListResult is a cursor-paginated page of assessments, matching the paging
// contract already used by the runs list.
type ListResult struct {
	Assessments []AssessmentSummary `json:"assessments"`
	Limit       int                 `json:"limit"`
	NextCursor  *time.Time          `json:"next_cursor,omitempty"`
	HasMore     bool                `json:"has_more"`
}

// ---- request payloads ----

// AnalyzeRequest asks for one report.
type AnalyzeRequest struct {
	Domain       string `json:"domain"` // slug
	SubjectTitle string `json:"subject_title"`
	Subject      string `json:"subject"`
	// Model may be empty, in which case the deployment's configured default is
	// used. Named explicitly when a caller is comparing a tuned adapter against
	// the base model on the same case.
	Model string `json:"model"`
}

// TrialRequest runs one case repeatedly to measure how stable the result is.
//
// This exists because "consistent scoring" is the product's central claim, and
// a claim like that is worth exactly as much as the number behind it. It is
// also how the effect of a PEFT build is proven rather than asserted: run the
// trials on the base model, run them again on the adapter, compare.
type TrialRequest struct {
	AnalyzeRequest
	Trials int `json:"trials"`
}

// TrialResult reports how a repeated case behaved.
type TrialResult struct {
	TrialGroup string `json:"trial_group"`
	Trials     int    `json:"trials"`

	// SchemaValidRate is the share of runs whose output parsed on the first
	// attempt. The headline number for whether this model can hold the schema.
	SchemaValidRate float64 `json:"schema_valid_rate"`

	ScoredRuns int      `json:"scored_runs"`
	MeanScore  *float64 `json:"mean_score"`
	// StdDevScore is the population standard deviation across trials. Reported
	// rather than a range because one outlier moves a range completely while
	// barely moving this.
	StdDevScore  *float64 `json:"stddev_score"`
	MinScore     *float64 `json:"min_score"`
	MaxScore     *float64 `json:"max_score"`
	MeanCoverage float64  `json:"mean_coverage"`

	// PerCriterionStdDev localises the instability: a rubric can be stable
	// overall while one criterion swings wildly, and that criterion's guidance
	// is what needs rewriting.
	PerCriterionStdDev map[string]float64 `json:"per_criterion_stddev"`

	// RedactedRuns is how many legs of this group have had their findings
	// blanked, by their owner or by the retention sweep.
	//
	// Reported because redaction is invisible in every other field here. A
	// redacted row keeps its score, its coverage and its schema_valid, so it
	// still counts in Trials, ScoredRuns and the ids — but its findings are
	// gone, and PerCriterionStdDev reads ratings out of findings. Three
	// redacted legs of five therefore produce a per-criterion spread computed
	// from two observations that looks exactly like one computed from five.
	// This number is what tells the two apart.
	RedactedRuns int `json:"redacted_runs"`

	AssessmentIDs []string `json:"assessment_ids"`
	MeanLatencyMs float64  `json:"mean_latency_ms"`
}

// rawFindings is the exact shape the model is asked to emit. It is separate
// from Finding so a malformed field fails at the edge, on decode, rather than
// somewhere deeper with a half-populated struct.
type rawFindings struct {
	Findings []struct {
		Key           string          `json:"key"`
		EvidenceFound *bool           `json:"evidence_found"`
		Score         json.RawMessage `json:"score"`
		// Raw because the model emits either a list of quotes or a single
		// string, and a []string here would fail the whole document's decode on
		// the string form — losing eight good findings to one field's shape.
		// Normalised by parseEvidence, which counts the string form against
		// strict schema validity while still keeping the quote.
		Evidence  json.RawMessage `json:"evidence"`
		Rationale string          `json:"rationale"`
	} `json:"findings"`
}
