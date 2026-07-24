// Package settings owns the runtime generation settings an administrator can
// change without redeploying: the system prompt, the sampling parameters, and
// which built adapter is currently being served.
//
// It is its own package, rather than living in either of its two users, to keep
// the dependency graph acyclic. The admin module *writes* these settings; the
// llm module *reads* them on every generation. Putting them in admin would make
// llm import admin, and putting them in llm would make admin's settings handler
// import the module it is meant to configure. A leaf package both can depend on
// costs one directory and removes the question entirely.
package settings

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Bounds are duplicated from the CHECK constraint in migration 004. That is
// deliberate: the constraint is the guarantee, and this is the good error
// message. A caller who sends temperature 5 should be told which field was
// wrong and what the range is, not handed a raw constraint-violation string.
const (
	MinTemperature = 0.0
	MaxTemperature = 2.0
	MinTopP        = 0.0 // exclusive
	MaxTopP        = 1.0
	MinMaxTokens   = 1
	MaxMaxTokens   = 8192

	// A system prompt occupies context on every single request — on a 2B model
	// with a small KV cache, a runaway one crowds out the actual conversation.
	// The cap is generous for a real prompt and hostile to a pasted document.
	MaxSystemPromptBytes = 8000
)

// Settings is the effective runtime configuration for generation.
//
// ActiveModelID is the field the inference path actually uses: it is the model
// id the host must be asked for. Empty means the untuned base model, which is
// also the state after the active adapter is deleted.
type Settings struct {
	SystemPrompt string  `json:"system_prompt"`
	Temperature  float64 `json:"temperature"`
	TopP         float64 `json:"top_p"`
	MaxTokens    int     `json:"max_tokens"`

	// DefaultModel is the operator's explicit model choice, independent of any
	// adapter. Empty means "no explicit choice", which falls through to the
	// active adapter and then to the compiled-in default. Separating the two
	// is what lets an operator run the untuned base deliberately while a tuned
	// build exists — the single most common thing to want during an evaluation.
	DefaultModel string `json:"default_model"`

	ActiveAdapterID   *string `json:"active_adapter_id"`
	ActiveAdapterName string  `json:"active_adapter_name"`
	ActiveModelID     string  `json:"active_model_id"`

	UpdatedAt time.Time `json:"updated_at"`
	UpdatedBy *string   `json:"updated_by"`
}

// Patch is a partial update. Every field is a pointer so that "not supplied"
// and "supplied as the zero value" stay distinguishable — without this, a
// client sending only a new system prompt would silently reset temperature to
// 0, which is a real behaviour change and not one anybody asked for.
type Patch struct {
	DefaultModel *string  `json:"default_model"`
	SystemPrompt *string  `json:"system_prompt"`
	Temperature  *float64 `json:"temperature"`
	TopP         *float64 `json:"top_p"`
	MaxTokens    *int     `json:"max_tokens"`
}

// ValidationError is a field the caller may fix, as opposed to a failure of
// ours. It exists as a type so handlers can tell the two apart with errors.As
// and answer 400 rather than 500 — matching on message text would work until
// the first database error whose string happened to start with a field name,
// and would then start reporting our outages as the client's mistake.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string { return e.Field + " " + e.Message }

// Validate reports the first field that is out of range.
func (p Patch) Validate() error {
	if p.DefaultModel != nil && len(*p.DefaultModel) > 200 {
		return &ValidationError{"default_model", "is too long to be a model id"}
	}
	if p.SystemPrompt != nil && len(*p.SystemPrompt) > MaxSystemPromptBytes {
		return &ValidationError{"system_prompt", fmt.Sprintf(
			"is %d bytes; the maximum is %d", len(*p.SystemPrompt), MaxSystemPromptBytes)}
	}
	if p.Temperature != nil && (*p.Temperature < MinTemperature || *p.Temperature > MaxTemperature) {
		return &ValidationError{"temperature", fmt.Sprintf(
			"must be between %.1f and %.1f", MinTemperature, MaxTemperature)}
	}
	if p.TopP != nil && (*p.TopP <= MinTopP || *p.TopP > MaxTopP) {
		return &ValidationError{"top_p", fmt.Sprintf(
			"must be greater than %.1f and at most %.1f", MinTopP, MaxTopP)}
	}
	if p.MaxTokens != nil && (*p.MaxTokens < MinMaxTokens || *p.MaxTokens > MaxMaxTokens) {
		return &ValidationError{"max_tokens", fmt.Sprintf(
			"must be between %d and %d", MinMaxTokens, MaxMaxTokens)}
	}
	return nil
}

// Store reads and writes the singleton settings row.
type Store struct {
	db *pgxpool.Pool
}

func NewStore(db *pgxpool.Pool) *Store { return &Store{db: db} }

// selectSQL is shared by Get and Update so the two can never drift into
// returning differently-shaped rows.
//
// The LEFT JOIN is what resolves an adapter id into something the inference
// host understands. It is a LEFT join, not an inner one, because the common
// case — no adapter active, base model being served — has no matching row and
// must still return the settings rather than nothing.
const selectSQL = `
	SELECT s.system_prompt, s.temperature, s.top_p, s.max_tokens,
	       s.default_model, s.active_adapter_id,
	       coalesce(a.name, ''), coalesce(a.mlc_model_id, ''),
	       s.updated_at, s.updated_by
	  FROM llm_settings s
	  LEFT JOIN llm_adapters a ON a.id = s.active_adapter_id
	 WHERE s.id = 1`

func scan(row pgx.Row) (Settings, error) {
	var s Settings
	err := row.Scan(&s.SystemPrompt, &s.Temperature, &s.TopP, &s.MaxTokens,
		&s.DefaultModel, &s.ActiveAdapterID, &s.ActiveAdapterName, &s.ActiveModelID,
		&s.UpdatedAt, &s.UpdatedBy)
	return s, err
}

// Get returns the current settings.
func (s *Store) Get(ctx context.Context) (Settings, error) {
	return scan(s.db.QueryRow(ctx, selectSQL))
}

// Update applies a partial change and returns the new state.
//
// COALESCE against the typed parameter, rather than building the SET clause
// from whichever fields are non-nil, keeps this one statement with one plan
// instead of a family of dynamically assembled ones. A NULL parameter means
// "leave this column alone", which is exactly what a nil pointer encodes.
func (s *Store) Update(ctx context.Context, p Patch, userID string) (Settings, error) {
	if err := p.Validate(); err != nil {
		return Settings{}, err
	}
	const q = `
		UPDATE llm_settings SET
		    system_prompt = COALESCE($1, system_prompt),
		    temperature   = COALESCE($2, temperature),
		    top_p         = COALESCE($3, top_p),
		    max_tokens    = COALESCE($4, max_tokens),
		    default_model = COALESCE($5, default_model),
		    updated_at    = now(),
		    updated_by    = $6
		 WHERE id = 1`
	if _, err := s.db.Exec(ctx, q, p.SystemPrompt, p.Temperature, p.TopP, p.MaxTokens,
		p.DefaultModel, userID); err != nil {
		return Settings{}, err
	}
	return s.Get(ctx)
}

// SetActiveAdapter points generation at a built adapter, or at the base model
// when id is nil.
//
// The guard is in SQL rather than in a read-then-write pair in Go: checking the
// status here means two administrators clicking "activate" at the same moment
// cannot both pass a check and have the loser's write land second. Referencing
// a row that is not `ready` or `active` matches nothing, updates nothing, and
// surfaces as ErrNotReady.
func (s *Store) SetActiveAdapter(ctx context.Context, id *string, userID string) (Settings, error) {
	if id == nil {
		const q = `UPDATE llm_settings
		              SET active_adapter_id = NULL, updated_at = now(), updated_by = $1
		            WHERE id = 1`
		if _, err := s.db.Exec(ctx, q, userID); err != nil {
			return Settings{}, err
		}
		return s.Get(ctx)
	}

	const q = `
		UPDATE llm_settings SET
		    active_adapter_id = $1,
		    updated_at = now(),
		    updated_by = $2
		 WHERE id = 1
		   AND EXISTS (
		       SELECT 1 FROM llm_adapters
		        WHERE id = $1 AND status IN ('ready', 'active') AND mlc_model_id <> ''
		   )`
	tag, err := s.db.Exec(ctx, q, *id, userID)
	if err != nil {
		return Settings{}, err
	}
	if tag.RowsAffected() == 0 {
		return Settings{}, ErrNotReady
	}
	return s.Get(ctx)
}

// ErrNotReady means the adapter exists but has not finished building, so there
// is nothing for the inference host to serve yet.
var ErrNotReady = errors.New("adapter is not built and ready")
