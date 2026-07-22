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

// Target is where a run executed. The same model id can run in the visitor's
// browser on WebGPU or on the self-hosted MLC server, and the two produce
// latencies that must never be averaged together: a browser figure describes
// whatever GPU the visitor happens to own, a server figure describes one fixed
// card plus a network hop.
const (
	TargetBrowser = "browser"
	TargetServer  = "server"
)

// Run is a single recorded LLM interaction (the "raw monitoring" record).
// Produced either in the browser by WebLLM and posted here, or by this service
// calling the MLC inference host; Target says which.
type Run struct {
	ID               string    `json:"id"`
	UserID           string    `json:"user_id"`
	Model            string    `json:"model"`
	Target           string    `json:"target"`
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
	Target           string    `json:"target"`
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

// CreateRunRequest is what the frontend posts after the browser's own engine
// answers. Unchanged by the arrival of server-side inference — the browser path
// still owns its own timings, so this contract did not have to break.
//
// Target is not read from the request: a client posting its own results is by
// definition the browser path, and letting it claim otherwise would corrupt the
// comparison this column exists for.
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

	// Target is set by the server, never decoded from the client. It is
	// unexported from the JSON contract on purpose.
	Target string `json:"-"`
}

// GenerateRunRequest asks this service to run the model itself and record the
// result. The response fields absent here — the answer, the token counts, the
// latency — are exactly the ones the server now produces rather than accepts.
type GenerateRunRequest struct {
	Model            string   `json:"model"`
	Prompt           string   `json:"prompt"`
	SystemPrompt     string   `json:"system_prompt"`
	Temperature      float64  `json:"temperature"`
	MaxTokens        int      `json:"max_tokens"`
	ExpectedKeywords []string `json:"expected_keywords"`
	AutoScore        bool     `json:"auto_score"`
}

// ScoreRequest can override the default weights when scoring a run.
type ScoreRequest struct {
	Weights *Weights `json:"weights,omitempty"`
}

// ---- Aggregate views for the dashboard ----

// Metrics is the aggregate summary shown on the monitoring dashboard.
//
// AvgLatencyMs and AvgCompletionTk are computed across every run regardless of
// where it executed, which makes them meaningless once both targets are in use:
// browser runs carry whatever GPU the visitor owns, server runs carry one fixed
// card plus a network hop. They are kept because clients already read them and
// because they are still correct while only one target has runs — but anything
// comparing performance should read ByTarget instead.
type Metrics struct {
	TotalRuns       int                      `json:"total_runs"`
	ScoredRuns      int                      `json:"scored_runs"`
	AvgScore        float64                  `json:"avg_score"`
	AvgLatencyMs    float64                  `json:"avg_latency_ms"`
	AvgCompletionTk float64                  `json:"avg_completion_tokens"`
	RunsByModel     map[string]int           `json:"runs_by_model"`
	GradeDistrib    map[string]int           `json:"grade_distribution"`
	ByTarget        map[string]TargetMetrics `json:"by_target"`
}

// TargetMetrics is one execution target's slice of the summary — the numbers
// that may only be compared within a target, never across one.
type TargetMetrics struct {
	Runs            int     `json:"runs"`
	AvgLatencyMs    float64 `json:"avg_latency_ms"`
	AvgCompletionTk float64 `json:"avg_completion_tokens"`
	AvgScore        float64 `json:"avg_score"`
	// TokensPerSecond is aggregate throughput: total tokens over total time,
	// not the mean of each run's rate. The mean of ratios would weight a
	// 3-token answer the same as a 400-token one and flatter whichever target
	// happened to get the short prompts.
	TokensPerSecond float64 `json:"tokens_per_second"`
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

// ModelInfo describes a model the frontend can offer, and where it can be run.
//
// Targets is additive: clients that predate it simply ignore the field and keep
// treating every entry as browser-runnable, which is what they already did.
type ModelInfo struct {
	ID          string   `json:"id"`
	Label       string   `json:"label"`
	Family      string   `json:"family"`
	SizeHint    string   `json:"size_hint"`
	Recommended bool     `json:"recommended"`
	Targets     []string `json:"targets"`
}
