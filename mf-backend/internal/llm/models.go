package llm

import "time"

// Run is a single recorded LLM interaction (the "raw monitoring" record).
// These are produced in the browser by WebLLM/Gemma and posted here.
type Run struct {
	ID               string         `json:"id"`
	UserID           string         `json:"user_id"`
	Model            string         `json:"model"`
	Prompt           string         `json:"prompt"`
	Response         string         `json:"response"`
	SystemPrompt     string         `json:"system_prompt"`
	PromptTokens     int            `json:"prompt_tokens"`
	CompletionTokens int            `json:"completion_tokens"`
	LatencyMs        int            `json:"latency_ms"`
	Temperature      float64        `json:"temperature"`
	ExpectedKeywords []string       `json:"expected_keywords"`
	Metadata         map[string]any `json:"metadata"`
	CreatedAt        time.Time      `json:"created_at"`
	Score            *Score         `json:"score,omitempty"`
}

// Score is the decision score for a run: an overall number plus a transparent
// breakdown of how it was reached. Transparency matters — a score nobody can
// explain is a score nobody trusts.
type Score struct {
	ID        string             `json:"id"`
	RunID     string             `json:"run_id"`
	Score     float64            `json:"score"`     // 0..100
	Grade     string             `json:"grade"`     // A..F
	Breakdown map[string]float64 `json:"breakdown"` // component -> 0..100
	Rationale string             `json:"rationale"`
	CreatedAt time.Time          `json:"created_at"`
}

// ---- Request payloads ----

// CreateRunRequest is what the frontend posts after Gemma answers.
type CreateRunRequest struct {
	Model            string         `json:"model"`
	Prompt           string         `json:"prompt"`
	Response         string         `json:"response"`
	SystemPrompt     string         `json:"system_prompt"`
	PromptTokens     int            `json:"prompt_tokens"`
	CompletionTokens int            `json:"completion_tokens"`
	LatencyMs        int            `json:"latency_ms"`
	Temperature      float64        `json:"temperature"`
	ExpectedKeywords []string       `json:"expected_keywords"`
	Metadata         map[string]any `json:"metadata"`
	AutoScore        bool           `json:"auto_score"` // score immediately on create
}

// ScoreRequest can override the default weights when scoring a run.
type ScoreRequest struct {
	Weights *Weights `json:"weights,omitempty"`
}

// ---- Aggregate views for the dashboard ----

// Metrics is the aggregate summary shown on the monitoring dashboard.
type Metrics struct {
	TotalRuns       int            `json:"total_runs"`
	ScoredRuns      int            `json:"scored_runs"`
	AvgScore        float64        `json:"avg_score"`
	AvgLatencyMs    float64        `json:"avg_latency_ms"`
	AvgCompletionTk float64        `json:"avg_completion_tokens"`
	RunsByModel     map[string]int `json:"runs_by_model"`
	GradeDistrib    map[string]int `json:"grade_distribution"`
}

// ListResult is a paginated list of runs.
type ListResult struct {
	Runs   []Run `json:"runs"`
	Total  int   `json:"total"`
	Limit  int   `json:"limit"`
	Offset int   `json:"offset"`
}

// ModelInfo describes a browser-runnable model the frontend can offer.
type ModelInfo struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Family      string `json:"family"`
	SizeHint    string `json:"size_hint"`
	Recommended bool   `json:"recommended"`
}
