package analysis

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/emrah/mf-backend/internal/common"
	"github.com/emrah/mf-backend/internal/llm"
	"github.com/emrah/mf-backend/internal/settings"
	"github.com/go-chi/chi/v5"
)

// defaultModel is used when neither the request nor the active adapter names
// one. It mirrors the catalogue entry in internal/llm rather than importing it,
// because the two mean different things: that one is what the UI offers, this
// one is what an API caller gets by omission.
const defaultModel = "gemma-2-2b-it-q4f16_1-MLC"

// analysisTemperature is pinned rather than taken from the operator's settings.
//
// Filling a rubric is extraction, not conversation: the same case must produce
// the same reading, and sampling variety is directly opposed to the consistency
// this product is sold on. Leaving it to a setting tuned for chat would let an
// unrelated slider quietly widen the score spread the trial endpoint measures.
// Not zero — greedy decoding on a small model degenerates into repetition,
// which fails the schema more often than a little sampling does.
const analysisTemperature = 0.1

// newUUID returns a random RFC 4122 version 4 UUID.
//
// Hand-rolled rather than adding a dependency for sixteen bytes. crypto/rand,
// not math/rand: a trial group id is used to address stored rows, so a
// predictable one would let a caller who can guess it read another user's
// trial — the ownership check in the store is the real defence, but a
// guessable identifier is not something to hand out on purpose.
func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failing means the system has no entropy source, which is
		// not a condition this service can sensibly continue through.
		panic("crypto/rand unavailable: " + err.Error())
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	s := hex.EncodeToString(b[:])
	return s[0:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:32]
}

// Limits on the case text. A pitch deck's extracted text is a few thousand
// words; anything far past that is a pasted PDF dump that will not fit the
// model's context anyway, and would be silently truncated by the engine rather
// than rejected here where the caller can be told.
const (
	maxSubjectBytes = 32000
	minSubjectBytes = 40

	// Trials above this stop being a measurement and start being a way to
	// occupy the one GPU for an hour. Ten runs is enough to see the spread.
	maxTrials     = 10
	defaultTrials = 5
)

// AssessmentStore is the persistence this handler needs, declared consumer-side
// so the handlers can be tested without PostgreSQL — the same shape as
// llm.RunStore, and named to leave `Store` for the concrete implementation.
type AssessmentStore interface {
	GetDomain(ctx context.Context, slug string) (Domain, error)
	ListDomains(ctx context.Context) ([]Domain, error)
	CreateAssessment(ctx context.Context, a Assessment) (Assessment, error)
	GetAssessment(ctx context.Context, userID, id string) (Assessment, error)
	ListAssessments(ctx context.Context, userID, domainSlug string, limit int, before time.Time) (ListResult, error)
	TrialAssessments(ctx context.Context, userID, group string) ([]Assessment, error)
}

// SettingsSource supplies the operator-tuned generation parameters. Reading
// them per request rather than at boot is what makes the admin panel's changes
// take effect without a redeploy of a service that runs on someone's desktop.
type SettingsSource interface {
	Get(ctx context.Context) (settings.Settings, error)
}

// Handler serves the analysis endpoints.
type Handler struct {
	store AssessmentStore
	gen   llm.Generator
	set   SettingsSource
}

func NewHandler(store AssessmentStore, gen llm.Generator, set SettingsSource) *Handler {
	return &Handler{store: store, gen: gen, set: set}
}

// Domains lists the rubrics on offer. GET /analysis/domains
func (h *Handler) Domains(w http.ResponseWriter, r *http.Request) {
	items, err := h.store.ListDomains(r.Context())
	if err != nil {
		common.Error(w, common.ErrInternal("could not list domains"))
		return
	}
	common.JSON(w, http.StatusOK, map[string]any{"domains": items, "count": len(items)})
}

// Prompt returns the exact instruction a domain generates.
// GET /analysis/domains/{slug}/prompt
//
// Exists for the fine-tuning pipeline, and it is not a convenience. A LoRA
// adapter learns to satisfy one specific prompt; if the trainer reproduces that
// prompt from its own copy of the template, the two drift the first time either
// is edited, and the adapter is then tuned for an instruction nothing sends.
// The failure is silent — training completes, the loss looks fine, and the
// model is simply worse at the job than the base it replaced.
//
// Serving it from the same function the inference path calls makes drift
// impossible rather than merely unlikely.
func (h *Handler) Prompt(w http.ResponseWriter, r *http.Request) {
	domain, err := h.store.GetDomain(r.Context(), chi.URLParam(r, "slug"))
	if errors.Is(err, ErrNoRows) {
		common.Error(w, common.ErrNotFound("no such analysis domain"))
		return
	}
	if err != nil {
		common.Error(w, common.ErrInternal("could not load the rubric"))
		return
	}
	common.JSON(w, http.StatusOK, map[string]any{
		"domain":        domain.Slug,
		"version":       domain.Version,
		"system_prompt": SystemPrompt(domain),
		// The user-message template, with a placeholder where the case goes.
		// Returned alongside because the trainer has to reproduce both halves,
		// and the quote neutralisation in particular is invisible from outside.
		"user_prompt_example": UserPrompt("{{title}}", "{{subject}}"),
		"criteria":            domain.Criteria,
		"temperature":         analysisTemperature,
	})
}

// Analyze produces one report. POST /analysis/run
func (h *Handler) Analyze(w http.ResponseWriter, r *http.Request) {
	claims, _ := common.ClaimsFromContext(r.Context())

	var req AnalyzeRequest
	if err := common.Decode(r, &req); err != nil {
		common.Error(w, err)
		return
	}
	domain, apiErr := h.prepare(r.Context(), &req)
	if apiErr != nil {
		common.Error(w, apiErr)
		return
	}

	a, err := h.runOne(r.Context(), claims.UserID, domain, req, nil)
	if err != nil {
		common.Error(w, err)
		return
	}
	common.JSON(w, http.StatusCreated, a)
}

// Trial runs one case repeatedly and reports how stable the result was.
// POST /analysis/trial
//
// This is the measurement the product's "consistent scoring" claim rests on,
// and the before/after instrument for a PEFT build. It is deliberately an
// endpoint rather than a script: the same case has to be runnable against the
// base model and a tuned adapter through exactly the same path, or the
// comparison measures the harness instead of the model.
func (h *Handler) Trial(w http.ResponseWriter, r *http.Request) {
	claims, _ := common.ClaimsFromContext(r.Context())

	var req TrialRequest
	if err := common.Decode(r, &req); err != nil {
		common.Error(w, err)
		return
	}
	domain, apiErr := h.prepare(r.Context(), &req.AnalyzeRequest)
	if apiErr != nil {
		common.Error(w, apiErr)
		return
	}
	if req.Trials <= 0 {
		req.Trials = defaultTrials
	}
	if req.Trials > maxTrials {
		common.Error(w, common.ErrBadRequest("trials may not exceed "+strconv.Itoa(maxTrials)))
		return
	}

	group := newUUID()
	result := TrialResult{TrialGroup: group, PerCriterionStdDev: map[string]float64{}}

	var (
		scores    []float64
		coverages []float64
		latencies []float64
		runs      [][]Finding
		valid     int
	)

	// Sequential, not concurrent. The replicas share one 6 GB card, so parallel
	// trials would contend for it and the latency figures would describe the
	// contention rather than the model. Correctness of the measurement beats
	// finishing it sooner.
	for i := 0; i < req.Trials; i++ {
		// A trial that fails part-way still reports what it managed. Discarding
		// four good runs because the fifth timed out would make the endpoint
		// useless on exactly the hardware it exists to measure.
		if err := r.Context().Err(); err != nil {
			slog.Warn("trial cut short", "group", group, "completed", len(result.AssessmentIDs))
			break
		}

		a, err := h.runOne(r.Context(), claims.UserID, domain, req.AnalyzeRequest, &group)
		if err != nil {
			slog.Warn("trial run failed", "group", group, "trial", i, "error", err)
			continue
		}
		result.AssessmentIDs = append(result.AssessmentIDs, a.ID)
		if a.SchemaValid {
			valid++
		}
		if a.OverallScore != nil {
			scores = append(scores, *a.OverallScore)
		}
		coverages = append(coverages, a.Coverage)
		latencies = append(latencies, float64(a.LatencyMs))
		runs = append(runs, a.Findings)
	}

	completed := len(result.AssessmentIDs)
	if completed == 0 {
		common.Error(w, common.ErrUpstreamFailed("every trial failed; nothing could be measured"))
		return
	}

	result.Trials = completed
	result.SchemaValidRate = round4(float64(valid) / float64(completed))
	result.ScoredRuns = len(scores)

	if mean, sd, min, max, ok := Stats(scores); ok {
		result.MeanScore = ptrRound(mean)
		result.StdDevScore = ptrRound(sd)
		result.MinScore = ptrRound(min)
		result.MaxScore = ptrRound(max)
	}
	if mean, _, _, _, ok := Stats(coverages); ok {
		result.MeanCoverage = round4(mean)
	}
	if mean, _, _, _, ok := Stats(latencies); ok {
		result.MeanLatencyMs = math.Round(mean)
	}
	result.PerCriterionStdDev = PerCriterionStdDev(domain.Criteria, runs)

	slog.Info("trial complete", "group", group, "trials", completed,
		"schema_valid_rate", result.SchemaValidRate, "stddev", result.StdDevScore)
	common.JSON(w, http.StatusOK, result)
}

// ---- service surface ----
//
// The methods below are the same work the HTTP handlers do, minus the request
// and response plumbing. They exist because the analysis engine now has a
// second caller — the MCP server — and that caller is not a browser.
//
// Deliberately thin wrappers rather than a duplicated pipeline. An MCP client
// and a browser must get identical reports from identical input, and the only
// way to be sure of that is for both to run the same code. A second
// implementation would drift on the first change to the prompt, the parser or
// the scoring, and the drift would show up as two customers disagreeing about
// what the same case scored.

// ExecuteAnalysis validates, runs and stores one analysis.
func (h *Handler) ExecuteAnalysis(
	ctx context.Context, userID string, req AnalyzeRequest,
) (Assessment, error) {
	domain, apiErr := h.prepare(ctx, &req)
	if apiErr != nil {
		return Assessment{}, apiErr
	}
	return h.runOne(ctx, userID, domain, req, nil)
}

// Catalogue returns the rubrics on offer.
func (h *Handler) Catalogue(ctx context.Context) ([]Domain, error) {
	return h.store.ListDomains(ctx)
}

// Report returns one stored report, scoped to its owner.
func (h *Handler) Report(ctx context.Context, userID, id string) (Assessment, error) {
	return h.store.GetAssessment(ctx, userID, id)
}

// Reports returns a page of the user's reports.
func (h *Handler) Reports(
	ctx context.Context, userID, domainSlug string, limit int,
) (ListResult, error) {
	return h.store.ListAssessments(ctx, userID, domainSlug, limit, time.Time{})
}

// prepare validates a request and resolves its rubric.
func (h *Handler) prepare(ctx context.Context, req *AnalyzeRequest) (Domain, *common.APIError) {
	if h.gen == nil || !h.gen.Configured() {
		return Domain{}, common.ErrUnavailable(
			"the inference host is not configured on this deployment")
	}
	req.Subject = strings.TrimSpace(req.Subject)
	req.SubjectTitle = strings.TrimSpace(req.SubjectTitle)

	if req.Domain == "" {
		return Domain{}, common.ErrBadRequest("domain is required")
	}
	if len(req.Subject) < minSubjectBytes {
		return Domain{}, common.ErrBadRequest(
			"subject is too short to analyse; at least " + strconv.Itoa(minSubjectBytes) + " characters are needed")
	}
	if len(req.Subject) > maxSubjectBytes {
		return Domain{}, common.ErrBadRequest(
			"subject exceeds " + strconv.Itoa(maxSubjectBytes) + " characters")
	}

	domain, err := h.store.GetDomain(ctx, req.Domain)
	if errors.Is(err, ErrNoRows) {
		return Domain{}, common.ErrNotFound("no such analysis domain")
	}
	if err != nil {
		return Domain{}, common.ErrInternal("could not load the rubric")
	}
	if len(domain.Criteria) == 0 {
		return Domain{}, common.ErrInternal("this rubric has no criteria")
	}
	return domain, nil
}

// runOne performs a single generation and stores the resulting report.
func (h *Handler) runOne(
	ctx context.Context, userID string, domain Domain, req AnalyzeRequest, group *string,
) (Assessment, error) {
	cfg, err := h.set.Get(ctx)
	if err != nil {
		return Assessment{}, common.ErrInternal("could not read settings")
	}

	// Precedence, most specific first: the request, then the operator's explicit
	// choice, then the active adapter's build, then the compiled default.
	//
	// The request wins so a base-versus-adapter comparison can run without an
	// operator flipping a global setting between the two halves — compare.py
	// depends on that. The operator's explicit choice sits *above* the active
	// adapter so the untuned base can be served deliberately while a tuned build
	// exists, which is the normal state during an evaluation.
	model := req.Model
	if model == "" {
		model = cfg.DefaultModel
	}
	if model == "" {
		model = cfg.ActiveModelID
	}
	if model == "" {
		model = defaultModel
	}

	completion, err := h.gen.Generate(ctx, llm.CompletionRequest{
		Model:        model,
		Prompt:       UserPrompt(req.SubjectTitle, req.Subject),
		SystemPrompt: SystemPrompt(domain),
		// Deliberately not the operator's chat temperature. Filling a rubric is
		// an extraction task, and sampling variety is the enemy of the
		// consistency this product is sold on — so it is pinned low here rather
		// than left to a setting tuned for conversational use.
		Temperature: analysisTemperature,
		MaxTokens:   tokenBudget(len(domain.Criteria), cfg.MaxTokens),
	})
	if err != nil {
		return Assessment{}, err
	}

	parsed, parseErr := Parse(completion.Content, domain.Criteria)
	if parseErr != nil {
		// The generation cost real GPU time, so the failure is recorded rather
		// than discarded: a stored row with schema_valid=false and the raw
		// response is exactly the evidence needed to justify fine-tuning.
		//
		// Truncation is called out separately because it is not the model's
		// failure and must never be counted as one. A run cut off by the budget
		// says nothing about whether the model can hold the schema, and folding
		// the two together produces a baseline that argues for fine-tuning a
		// problem fine-tuning would not fix.
		slog.Warn("model output did not parse",
			"domain", domain.Slug, "model", model,
			"truncated", completion.Truncated, "error", parseErr)
	}
	if completion.Truncated {
		slog.Warn("answer hit the token budget; this run does not measure the model",
			"domain", domain.Slug, "criteria", len(domain.Criteria),
			"completion_tokens", completion.CompletionTokens)
		parsed.Problems = append(parsed.Problems, "answer was truncated by the token budget")
		parsed.SchemaValid = false
	}
	if len(parsed.Problems) > 0 {
		slog.Info("schema problems", "domain", domain.Slug, "model", model,
			"count", len(parsed.Problems), "first", parsed.Problems[0])
	}

	overall, coverage := Score(domain.Criteria, parsed.Findings)

	a := Assessment{
		UserID:           userID,
		DomainID:         domain.ID,
		DomainSlug:       domain.Slug,
		DomainName:       domain.Name,
		DomainVersion:    domain.Version,
		CriteriaSnapshot: domain.Criteria,
		SubjectTitle:     req.SubjectTitle,
		Subject:          req.Subject,
		Findings:         parsed.Findings,
		OverallScore:     overall,
		Coverage:         coverage,
		SchemaValid:      parsed.SchemaValid,
		RepairAttempts:   parsed.RepairAttempts,
		TrialGroup:       group,
		Model:            model,
		Target:           llm.TargetServer,
		AdapterID:        cfg.ActiveAdapterID,
		LatencyMs:        completion.LatencyMs,
		PromptTokens:     completion.PromptTokens,
		CompletionTokens: completion.CompletionTokens,
		RawResponse:      completion.Content,
	}
	if a.Findings == nil {
		a.Findings = []Finding{}
	}

	saved, err := h.store.CreateAssessment(ctx, a)
	if err != nil {
		slog.Error("analysed but could not save", "user_id", userID, "error", err)
		return Assessment{}, common.ErrInternal("could not save the report")
	}
	a.ID, a.CreatedAt = saved.ID, saved.CreatedAt
	return a, nil
}

// Get returns one report. GET /analysis/{id}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	claims, _ := common.ClaimsFromContext(r.Context())
	a, err := h.store.GetAssessment(r.Context(), claims.UserID, chi.URLParam(r, "id"))
	if errors.Is(err, ErrNoRows) {
		common.Error(w, common.ErrNotFound("report not found"))
		return
	}
	if err != nil {
		common.Error(w, common.ErrInternal("could not read the report"))
		return
	}
	common.JSON(w, http.StatusOK, a)
}

// List returns a page of the user's reports. GET /analysis
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	claims, _ := common.ClaimsFromContext(r.Context())
	q := r.URL.Query()

	limit := 20
	if n, err := strconv.Atoi(q.Get("limit")); err == nil {
		limit = n
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 100 {
		limit = 100
	}

	var before time.Time
	if raw := q.Get("before"); raw != "" {
		t, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			common.Error(w, common.ErrBadRequest("before must be an RFC3339 timestamp"))
			return
		}
		before = t
	}

	res, err := h.store.ListAssessments(r.Context(), claims.UserID, q.Get("domain"), limit, before)
	if err != nil {
		common.Error(w, common.ErrInternal("could not list reports"))
		return
	}
	common.JSON(w, http.StatusOK, res)
}

// TrialGroup re-reads a completed consistency run. GET /analysis/trials/{group}
func (h *Handler) TrialGroup(w http.ResponseWriter, r *http.Request) {
	claims, _ := common.ClaimsFromContext(r.Context())
	group := chi.URLParam(r, "group")

	items, err := h.store.TrialAssessments(r.Context(), claims.UserID, group)
	if err != nil {
		common.Error(w, common.ErrInternal("could not read the trial"))
		return
	}
	if len(items) == 0 {
		common.Error(w, common.ErrNotFound("no such trial"))
		return
	}
	common.JSON(w, http.StatusOK, map[string]any{
		"trial_group": group,
		"assessments": items,
		"summary":     summarise(group, items),
	})
}

// summarise recomputes a trial's statistics from stored rows, so a past trial
// can be re-read without having been kept in memory.
func summarise(group string, items []Assessment) TrialResult {
	out := TrialResult{TrialGroup: group, Trials: len(items)}
	var scores, coverages, latencies []float64
	var runs [][]Finding
	valid := 0

	for _, a := range items {
		if a.SchemaValid {
			valid++
		}
		if a.OverallScore != nil {
			scores = append(scores, *a.OverallScore)
		}
		coverages = append(coverages, a.Coverage)
		latencies = append(latencies, float64(a.LatencyMs))
		runs = append(runs, a.Findings)
		out.AssessmentIDs = append(out.AssessmentIDs, a.ID)
	}

	out.SchemaValidRate = round4(float64(valid) / float64(len(items)))
	out.ScoredRuns = len(scores)
	if mean, sd, min, max, ok := Stats(scores); ok {
		out.MeanScore, out.StdDevScore = ptrRound(mean), ptrRound(sd)
		out.MinScore, out.MaxScore = ptrRound(min), ptrRound(max)
	}
	if mean, _, _, _, ok := Stats(coverages); ok {
		out.MeanCoverage = round4(mean)
	}
	if mean, _, _, _, ok := Stats(latencies); ok {
		out.MeanLatencyMs = math.Round(mean)
	}
	// Criteria are read from the first row's snapshot: every run in a group
	// shares one rubric, and the snapshot is the version actually used.
	if len(items) > 0 {
		out.PerCriterionStdDev = PerCriterionStdDev(items[0].CriteriaSnapshot, runs)
	}
	return out
}

// tokenBudget returns a completion limit that can actually hold the answer.
//
// This is a floor, not an override: the operator's setting wins whenever it is
// already generous enough. It exists because the two numbers are not
// independent, and the failure when they disagree is silent and badly
// misleading.
//
// Generation stops at the limit mid-object, the JSON never closes, every report
// comes back schema_valid=false, and the conclusion drawn would be "the base
// model cannot hold the schema" when the truth is "we cut it off". That bogus
// baseline would then be the number a fine-tuning decision is made against, so
// the mistake compounds rather than staying local.
//
// perCriterion was 110 on the first pass and that was measured wrong: with the
// prompt now capping quotes at 200 characters, a finding still costs roughly
// 260 output tokens — two short quotes in Turkish, a sentence of rationale, and
// the JSON scaffolding around them. Turkish is the reason the figure is not
// smaller; its agglutinative forms tokenise poorly under a mostly-English
// vocabulary, so the same sentence costs noticeably more tokens than its
// English equivalent. The estimate is deliberately generous: overshooting wastes
// nothing, because generation stops when the model closes the object.
//
// Capped because the ceiling is real: these replicas share one 6 GB card in
// `--mode local`, whose KV cache is small, and an unbounded budget buys a
// timeout rather than a longer answer.
func tokenBudget(criteria, configured int) int {
	const (
		perCriterion = 260
		overhead     = 200
		ceiling      = 4096
	)
	need := criteria*perCriterion + overhead
	if need > ceiling {
		need = ceiling
	}
	if configured > need {
		return configured
	}
	return need
}

func round4(f float64) float64 { return math.Round(f*10000) / 10000 }

func ptrRound(f float64) *float64 {
	v := math.Round(f*100) / 100
	return &v
}
