package llm

import (
	"encoding/json"
	"time"
)

// Metadata is free-form context the browser attaches to a run (device, WebGPU
// adapter, and so on). The server only stores and returns it — it never reads
// inside — so it is carried as raw JSON rather than decoded into a
// map[string]any. Decoding would box every leaf value into an interface and
// allocate a fresh nested map per object, which is the most GC-hostile shape
// of garbage there is, all to reproduce bytes we already had.
type Metadata = json.RawMessage

// Run is a single recorded LLM interaction (the "raw monitoring" record).
// These are produced in the browser by WebLLM/Gemma and posted here.
type Run struct {
	ID               string    `json:"id"`
	UserID           string    `json:"user_id"`
	Model            string    `json:"model"`
	Prompt           string    `json:"prompt"`
	Response         string    `json:"response"`
	SystemPrompt     string    `json:"system_prompt"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	LatencyMs        int       `json:"latency_ms"`
	Temperature      float64   `json:"temperature"`
	ExpectedKeywords []string  `json:"expected_keywords"`
	Metadata         Metadata  `json:"metadata"`
	CreatedAt        time.Time `json:"created_at"`
	Score            *Score    `json:"score,omitempty"`
}

// RunSummary is the list-view projection of a Run. The large text columns
// (prompt, response, system_prompt) are deliberately absent: the history list
// renders none of them in full, and fetching them made every page pay for TOAST
// decompression, heap copies and JSON encoding of data nobody displayed.
// PromptPreview is truncated in SQL so the full column is never read.
type RunSummary struct {
	ID               string    `json:"id"`
	Model            string    `json:"model"`
	PromptPreview    string    `json:"prompt_preview"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	LatencyMs        int       `json:"latency_ms"`
	CreatedAt        time.Time `json:"created_at"`
	Score            *Score    `json:"score,omitempty"`
}

// Score is the decision score for a run: an overall number plus a transparent
// breakdown of how it was reached. Transparency matters — a score nobody can
// explain is a score nobody trusts.
type Score struct {
	ID        string    `json:"id"`
	RunID     string    `json:"run_id"`
	Score     float64   `json:"score"` // 0..100
	Grade     string    `json:"grade"` // A..F
	Breakdown Breakdown `json:"breakdown"`
	Rationale string    `json:"rationale"`
	CreatedAt time.Time `json:"created_at"`
}

// Breakdown carries the per-component scores, each 0..100.
//
// This is a struct rather than a map[string]float64 because the set of
// components is fixed and known at compile time. A map costs an allocation per
// score, hashing on every read, and gives up any compile-time guarantee that a
// component name is spelled correctly. The JSON shape is identical, so this is
// invisible to clients.
type Breakdown struct {
	Completion float64 `json:"completion"`
	Latency    float64 `json:"latency"`
	Efficiency float64 `json:"efficiency"`
	Keywords   float64 `json:"keywords"`
	Length     float64 `json:"length"`
}

// ---- Request payloads ----

// CreateRunRequest is what the frontend posts after Gemma answers.
type CreateRunRequest struct {
	Model            string   `json:"model"`
	Prompt           string   `json:"prompt"`
	Response         string   `json:"response"`
	SystemPrompt     string   `json:"system_prompt"`
	PromptTokens     int      `json:"prompt_tokens"`
	CompletionTokens int      `json:"completion_tokens"`
	LatencyMs        int      `json:"latency_ms"`
	Temperature      float64  `json:"temperature"`
	ExpectedKeywords []string `json:"expected_keywords"`
	Metadata         Metadata `json:"metadata"`
	AutoScore        bool     `json:"auto_score"` // score immediately on create
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

// ListResult is a page of runs using keyset (seek) pagination.
//
// NextCursor is the created_at of the last row, to be passed back as ?before=
// for the following page; it is present only when HasMore is true. This
// replaces LIMIT/OFFSET: OFFSET must generate and discard every skipped row, so
// its cost grows with page depth — measured at 36ms and a 4.8MB temp-file spill
// by page 150, versus a flat 0.3ms here.
//
// Total is gone by design. It required a second full aggregate query on every
// page, and the same number is already published by GET /llm/metrics.
type ListResult struct {
	Runs       []RunSummary `json:"runs"`
	Limit      int          `json:"limit"`
	NextCursor *time.Time   `json:"next_cursor,omitempty"`
	HasMore    bool         `json:"has_more"`
}

// ModelInfo describes a browser-runnable model the frontend can offer.
type ModelInfo struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Family      string `json:"family"`
	SizeHint    string `json:"size_hint"`
	Recommended bool   `json:"recommended"`
}
