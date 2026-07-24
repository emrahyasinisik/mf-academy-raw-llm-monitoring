// Package admin is the control plane: the endpoints behind the operator's
// panel. Everything here is gated on the admin role and none of it is reachable
// by an ordinary user.
//
// The panel's four modules map onto three concerns in this package — adapter
// builds, the settings passthrough (owned by internal/settings), and the log
// monitor — because "system prompt" and "context limits" are two groups of
// fields on one settings row, not two subsystems.
package admin

import (
	"encoding/json"
	"fmt"
	"time"
)

// Adapter build lifecycle.
//
// A row moves forward through these and can drop to StatusFailed from any of
// the working states. It is a small explicit state machine rather than a
// boolean "ready" flag because the interesting question during a build is
// *which stage is running* — merging and compiling have very different
// durations and very different failure modes, and an operator watching a
// progress bar needs to be told them apart.
const (
	StatusRegistered = "registered"
	StatusTraining   = "training"
	StatusMerging    = "merging"
	StatusCompiling  = "compiling"
	StatusReady      = "ready"
	StatusActive     = "active"
	StatusFailed     = "failed"
)

// allowedTransitions is the state machine, written out rather than inferred.
//
// The pipeline runs outside this service (on the GPU box) and reports progress
// back over HTTP, so these transitions are asserted against a caller we do not
// control. Without the check, a retried or out-of-order callback could walk a
// finished build backwards into `training` and leave the panel showing a build
// that is not happening.
var allowedTransitions = map[string][]string{
	StatusRegistered: {StatusTraining, StatusFailed},
	StatusTraining:   {StatusMerging, StatusFailed},
	StatusMerging:    {StatusCompiling, StatusFailed},
	StatusCompiling:  {StatusReady, StatusFailed},
	// A ready build can be retried from scratch, so training is reachable again.
	StatusReady:  {StatusActive, StatusTraining, StatusFailed},
	StatusActive: {StatusReady, StatusTraining, StatusFailed},
	StatusFailed: {StatusTraining, StatusRegistered},
}

// ValidTransition reports whether a build may move from one state to another.
// A no-op transition is allowed so a pipeline that re-sends its last update
// (a retry, a duplicate webhook) is idempotent rather than an error.
func ValidTransition(from, to string) bool {
	if from == to {
		return true
	}
	for _, s := range allowedTransitions[from] {
		if s == to {
			return true
		}
	}
	return false
}

// TerminalStatuses are the states a build stops in.
func IsTerminal(status string) bool {
	return status == StatusReady || status == StatusActive || status == StatusFailed
}

// Adapter is one PEFT build.
//
// The row is the reproducible record of a build, which is why the LoRA
// hyperparameters are stored alongside the result: an adapter whose rank and
// target modules are unknown cannot be rebuilt, compared against, or explained
// six weeks later.
type Adapter struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	BaseModel     string   `json:"base_model"`
	Status        string   `json:"status"`
	LoRARank      int      `json:"lora_rank"`
	LoRAAlpha     int      `json:"lora_alpha"`
	TargetModules []string `json:"target_modules"`
	MLCModelID    string   `json:"mlc_model_id"`
	// GGUFAdapter is the bare file name published to the hot-swap runtime.
	// Independent of MLCModelID rather than an alternative to it: the same
	// training run can produce both artefacts, and which of them exists decides
	// whether activating this build is instant or needs a rebuild.
	GGUFAdapter string          `json:"gguf_adapter"`
	Metrics     json.RawMessage `json:"metrics"`
	Notes       string          `json:"notes"`
	LastError   string          `json:"last_error"`
	CreatedBy   *string         `json:"created_by"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	ActivatedAt *time.Time      `json:"activated_at"`
}

// CreateAdapterRequest registers a build. It does not start one — training runs
// on the GPU box, which this service cannot reach; see StartTraining in the
// MCP tools for how a build is actually kicked off.
type CreateAdapterRequest struct {
	Name          string   `json:"name"`
	BaseModel     string   `json:"base_model"`
	LoRARank      int      `json:"lora_rank"`
	LoRAAlpha     int      `json:"lora_alpha"`
	TargetModules []string `json:"target_modules"`
	Notes         string   `json:"notes"`
}

// defaultTargetModules is the standard attention-only LoRA placement.
//
// Deliberately excludes the embedding layer. Gemma's vocabulary is 256k tokens
// against a hidden size of 2304, so its embedding matrix is roughly a fifth of
// the whole model — adapting it would spend most of the memory budget that
// makes training on a 6 GB card possible in the first place.
var defaultTargetModules = []string{"q_proj", "k_proj", "v_proj", "o_proj"}

// knownTargetModules bounds what may be requested. The set is the projection
// names in a Gemma-2 decoder layer; anything else is a typo that would produce
// an adapter training zero parameters and a build that "succeeds" having
// learned nothing.
var knownTargetModules = map[string]bool{
	"q_proj": true, "k_proj": true, "v_proj": true, "o_proj": true,
	"gate_proj": true, "up_proj": true, "down_proj": true,
}

const (
	maxLoRARank  = 256
	maxLoRAAlpha = 512
	maxNameLen   = 64
)

// Normalize fills defaults and reports what cannot be repaired.
func (r *CreateAdapterRequest) Normalize() error {
	if r.Name == "" {
		return fmt.Errorf("name is required")
	}
	if len(r.Name) > maxNameLen {
		return fmt.Errorf("name is %d characters; the maximum is %d", len(r.Name), maxNameLen)
	}
	if r.BaseModel == "" {
		return fmt.Errorf("base_model is required")
	}
	if r.LoRARank == 0 {
		r.LoRARank = 16
	}
	if r.LoRAAlpha == 0 {
		// alpha = 2r is the common default. The effective update is scaled by
		// alpha/r, so tying alpha to rank keeps that scale constant when rank
		// changes — otherwise raising rank quietly weakens every update.
		r.LoRAAlpha = 2 * r.LoRARank
	}
	if r.LoRARank < 1 || r.LoRARank > maxLoRARank {
		return fmt.Errorf("lora_rank must be between 1 and %d", maxLoRARank)
	}
	if r.LoRAAlpha < 1 || r.LoRAAlpha > maxLoRAAlpha {
		return fmt.Errorf("lora_alpha must be between 1 and %d", maxLoRAAlpha)
	}
	if len(r.TargetModules) == 0 {
		r.TargetModules = defaultTargetModules
	}
	for _, m := range r.TargetModules {
		if !knownTargetModules[m] {
			return fmt.Errorf("unknown target module %q", m)
		}
	}
	return nil
}

// UpdateStatusRequest is what the build pipeline posts back as it progresses.
type UpdateStatusRequest struct {
	Status      string          `json:"status"`
	MLCModelID  string          `json:"mlc_model_id"`
	GGUFAdapter string          `json:"gguf_adapter"`
	Metrics     json.RawMessage `json:"metrics"`
	Error       string          `json:"error"`
}

// LogEntry is one row of the operator's log monitor: a run, with the identity
// of whoever caused it.
//
// This is the one place in the API where a user's data is visible to somebody
// who is not that user, so it carries only what operating the system requires —
// timings, sizes, model, outcome — and deliberately not the prompt or the
// response. An operator debugging latency does not need to read what people
// asked, and a log that contains it becomes a liability the moment it is
// exported anywhere.
type LogEntry struct {
	ID               string    `json:"id"`
	UserEmail        string    `json:"user_email"`
	Model            string    `json:"model"`
	Target           string    `json:"target"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	LatencyMs        int       `json:"latency_ms"`
	Score            *float64  `json:"score"`
	Grade            string    `json:"grade"`
	CreatedAt        time.Time `json:"created_at"`
}

// LogPage is a cursor-paginated slice of the log monitor, matching the paging
// contract the runs list already uses.
type LogPage struct {
	Entries    []LogEntry `json:"entries"`
	Limit      int        `json:"limit"`
	NextCursor *time.Time `json:"next_cursor,omitempty"`
	HasMore    bool       `json:"has_more"`
}

// Overview is the panel's header: the few numbers an operator wants before
// reading anything in detail.
type Overview struct {
	TotalUsers   int     `json:"total_users"`
	TotalRuns    int     `json:"total_runs"`
	RunsLast24h  int     `json:"runs_last_24h"`
	AvgLatencyMs float64 `json:"avg_latency_ms_24h"`
	P95LatencyMs float64 `json:"p95_latency_ms_24h"`

	AdaptersTotal int `json:"adapters_total"`
	AdaptersReady int `json:"adapters_ready"`

	Assessments        int `json:"assessments"`
	AssessmentsLast24h int `json:"assessments_last_24h"`
	// SchemaValidRate over the last 24 hours is the operating health of the
	// product, not a curiosity: every report whose output did not parse is one
	// a customer either did not get or should not trust. A drop here after an
	// adapter is activated is the signal to roll back, and it is the only
	// number on this panel that says so.
	SchemaValidRate24h float64 `json:"schema_valid_rate_24h"`

	ActiveAdapterID *string `json:"active_adapter_id"`
}
