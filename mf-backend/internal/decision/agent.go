// Package decision is the investment persona: one agent that researches a
// market, brand, product or technology live and reaches an investability
// verdict. It is not a second model — it is an orchestration around the same
// inference host the rest of the system uses.
//
// The orchestration is deliberately backend-driven rather than model-driven.
// The served model is small (a 2B Gemma), and small models are unreliable at
// deciding for themselves which tool to call and when. So the Go code runs the
// tools — a live web search and the DeepKwiki knowledge base — on every turn,
// hands the model the evidence it collected, and asks it to do the one thing it
// is good at: reason over what is in front of it and either ask a single
// clarifying question or commit to a verdict. The model never chooses a tool;
// it only ever sees results.
package decision

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/emrah/mf-backend/internal/llm"
	"github.com/emrah/mf-backend/internal/settings"
	"github.com/emrah/mf-backend/internal/wiki"
)

// Chatter is the multi-turn half of the LLM provider. Declared here, satisfied
// by *llm.OpenAIProvider, so the agent depends on the capability it needs and
// not on the concrete provider.
type Chatter interface {
	Chat(ctx context.Context, model string, messages []llm.Message, temperature float64, maxTokens int) (llm.Completion, error)
}

// WikiRetriever is the DeepKwiki corpus as a tool: retrieval only, no new
// generation. Satisfied by *wiki.Store.
type WikiRetriever interface {
	Search(ctx context.Context, query string, limit int) ([]wiki.Hit, error)
}

// SettingsSource supplies the operator's live model choice, read per request so
// activating an adapter in the admin panel takes effect on the next turn.
type SettingsSource interface {
	Get(ctx context.Context) (settings.Settings, error)
}

// Agent wires the persona to its tools.
type Agent struct {
	chat     Chatter
	search   Searcher
	wiki     WikiRetriever
	settings SettingsSource
	// promptChars is the whole input budget for one turn, in characters. See
	// promptPlan for why this agent budgets its prompt at all and why it does so
	// in characters rather than tokens.
	promptChars int
	// llmTimeout is the generation slice of one turn, separate from the route
	// budget that also pays for web search and wiki retrieval.
	llmTimeout time.Duration
}

// NewAgent takes the engine's input window in tokens, not because the agent
// counts tokens but because that is the number the operator can actually read
// off the inference host.
func NewAgent(chat Chatter, search Searcher, retriever WikiRetriever, set SettingsSource, maxPromptTokens int, llmTimeout time.Duration) *Agent {
	if maxPromptTokens <= 0 {
		maxPromptTokens = fallbackPromptTokens
	}
	if llmTimeout <= 0 {
		llmTimeout = 120 * time.Second
	}
	return &Agent{
		chat:        chat,
		search:      search,
		wiki:        retriever,
		settings:    set,
		promptChars: int(float64(maxPromptTokens) * charsPerToken),
		llmTimeout:  llmTimeout,
	}
}

// defaultModel matches the analysis and wiki paths: the fallback when neither
// the operator's settings name a model nor an adapter is active.
//
// The Qwen build rather than the Gemma one because it is what the inference host
// serves. mlc_llm does not validate this field — it answers from whatever single
// model it loaded and echoes the requested id back — so a stale value here does
// not fail, which is worse than failing: every stored row and every chart then
// attributes the run to a model that was not involved.
const defaultModel = "qwen3-4b-instruct-q4f16_1-MLC"

// personaTemperature keeps the verdict grounded. This is an analyst, not a
// brainstorm; a low temperature is what stops it inventing evidence.
const personaTemperature = 0.2

// Raised from 1024 because Qwen3 may spend tokens in a leading reasoning
// block before the KARAR/SKOR lines the UI parses.
const personaMaxTokens = 1536

// Bounds on how much evidence goes into one prompt. The card has 6 GB; an
// unbounded evidence block is the fastest way to run the KV cache out of it.
//
// These cap how many sources are *fetched*. How many survive into the prompt is
// decided separately by promptPlan, because the engine's limit is on characters
// of prompt and not on count of sources.
const (
	maxWebResults  = 5
	maxWikiResults = 4
	maxSnippetLen  = 500
)

// wikiProvider names the retrieval side in a ResearchStep. The web side asks its
// Searcher for a name because that one can change with configuration; this one
// cannot — there is a single corpus behind it.
const wikiProvider = "deepkwiki"

// The prompt budget.
//
// The engine enforces a hard input limit and rejects an over-long prompt with a
// 400 before generating anything, so a turn that does not fit does not produce a
// short answer — it produces no answer. This persona is the one path that can
// exceed it without anybody writing a long message: it re-sends the whole
// conversation and staples a fresh evidence block to every turn, so the prompt
// grows on its own until the turn stops working.
const (
	// charsPerToken converts the operator-visible token limit into the unit the
	// trimming is done in. Characters, because the tokeniser lives on the
	// inference host and the only alternative — asking it how long a prompt is
	// before sending it — is a network round trip per trim step.
	//
	// 2.2 is below the 2.75 measured on a real persona prompt (Turkish prose,
	// URLs and rubric text together) on purpose. Dense Turkish tokenises worse
	// than that average because each diacritic tends to cost its own token, and
	// the two errors are not symmetric: estimating low drops a source, and
	// estimating high loses the turn.
	charsPerToken = 2.2

	// promptMarginChars covers what this side cannot count: the chat template's
	// own turn markers, and the role tokens the engine adds per message.
	promptMarginChars = 400

	// evidenceShare is how much of the free budget goes to this turn's evidence
	// before any of it goes to conversation history. Evidence is what the verdict
	// has to be grounded in and it was gathered for this question; an older turn
	// is context the user can restate. So history is what gets dropped first.
	evidenceShare = 0.75

	// fallbackPromptTokens applies when the caller passes nothing usable. Small
	// enough to fit every window this stack has served.
	fallbackPromptTokens = 1200

	// citationOverhead is the fixed cost of one source's citation line — the
	// "[12] (DeepKwiki) " prefix, the separator and the two newlines — charged
	// on top of its title and URL so a source is never admitted on a budget its
	// own scaffolding already spent.
	citationOverhead = 24
)

// evidenceHeader opens the evidence block. Named so the fine-tune's dataset
// builder can read the exact line from GET /decision/prompt rather than
// reproduce it and risk drifting from what the agent actually sends.
const evidenceHeader = "KANITLAR (yalnızca bunlara dayan, uydurma):"

// Turn is one message in the conversation, as the client stores it. The last
// turn is always the new user message the agent must answer.
type Turn struct {
	Role    string `json:"role"` // "user" | "assistant"
	Content string `json:"content"`
}

// Source is one piece of evidence the persona was given, numbered so the reply
// can cite it as [n] and the reader can check it.
type Source struct {
	N     int    `json:"n"`
	Title string `json:"title"`
	URL   string `json:"url"`
	Kind  string `json:"kind"` // "web" | "wiki"
}

// ResearchStep records a tool the persona ran this turn, so the UI can show
// that the verdict came from live research rather than the model's memory.
//
// Results alone cannot carry that claim. Zero results is two different facts —
// the tool ran and the subject has no coverage, or the tool never answered —
// and for a while the screen showed both as "0". A keyless DuckDuckGo scrape
// blocked at the datacentre IP and an unseeded DeepKwiki corpus both read as
// "nothing out there about this company", which is the one reading that makes
// an empty verdict look like a measurement. Error separates them; Provider says
// which implementation produced the number, so a thin answer can be traced to a
// keyless fallback rather than argued about.
type ResearchStep struct {
	Tool     string `json:"tool"`
	Query    string `json:"query"`
	Results  int    `json:"results"`
	Provider string `json:"provider"`
	// Error is the tool's failure, already flattened to a string for the UI.
	// Empty when the tool answered — including when it answered with nothing.
	Error string `json:"error,omitempty"`
}

// Result is one reply from the persona.
type Result struct {
	Reply    string         `json:"reply"`
	Sources  []Source       `json:"sources"`
	Research []ResearchStep `json:"research"`
	Model    string         `json:"model"`
}

// Respond runs one turn: research the subject live, then let the model reason
// over the evidence. history includes the new user message as its last element.
func (a *Agent) Respond(ctx context.Context, history []Turn) (Result, error) {
	if len(history) == 0 {
		return Result{}, fmt.Errorf("no conversation")
	}
	latest := history[len(history)-1]
	if strings.TrimSpace(latest.Content) == "" {
		return Result{}, fmt.Errorf("empty message")
	}

	// Intake often says Konu: marka while the brand lives in the question
	// (hepsiburada.com). researchQueries pulls the entity out so live search
	// is about the brand, not the word "marka".
	primary, fallback := researchQueries(history, latest.Content)

	// The budget is decided before the evidence is gathered, not after. gather
	// numbers each source as it writes it, so a source dropped afterwards would
	// leave the reply citing [4] while the UI lists three sources — a citation
	// pointing at nothing, in the one feature whose whole claim is that every
	// claim is checkable.
	plan := a.plan(history)
	sources, evidence, steps := a.gather(ctx, primary, fallback, plan.evidence)

	model := a.resolveModel(ctx)
	messages := a.compose(history, evidence, plan)

	// Reserve the full generation budget even when search already consumed part
	// of the route deadline. Without this, a 20s search under a 90s route left
	// only ~70s for the model and surfaced as "inference host did not answer in
	// time" even though the host was still generating.
	llmCtx, cancel := context.WithTimeout(ctx, a.llmTimeout)
	defer cancel()

	comp, err := a.chat.Chat(llmCtx, model, messages, personaTemperature, personaMaxTokens)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Reply:    strings.TrimSpace(comp.Content),
		Sources:  sources,
		Research: steps,
		Model:    model,
	}, nil
}

// gather runs both tools and assembles a numbered evidence block plus the
// source list that lets the reader check every citation. A tool that fails is
// logged into the evidence as unavailable rather than aborting the turn: a
// verdict on thin evidence, clearly labelled, beats no verdict at all.
//
// budget is the characters the evidence block may occupy. Sources are admitted
// until it runs out, and the last one admitted has its snippet shortened to fit
// rather than being dropped whole — a title and a URL the reader can follow is
// worth more than the space it costs. Both returns stay in lockstep: a source
// that did not fit the prompt is not in the list either, so the numbering the
// model cites and the numbering the UI shows are the same numbering.
func (a *Agent) gather(ctx context.Context, query, fallback string, budget int) ([]Source, string, []ResearchStep) {
	var (
		sources []Source
		b       strings.Builder
		steps   []ResearchStep
		n       int
		dropped int
	)

	b.WriteString(evidenceHeader + "\n")

	// room reports what is left for one more source's snippet, after the fixed
	// cost of its own citation line.
	room := func(fixed int) int { return budget - b.Len() - fixed }

	web, err := a.search.Search(ctx, query, maxWebResults)
	webStep := ResearchStep{Tool: "web_research", Query: query, Results: len(web), Provider: a.search.Name()}
	switch {
	case err != nil:
		// The prompt gets the fact, not the error text: the model cannot act on
		// "403" and the string would spend evidence budget. What it must not do
		// is read a broken tool as a quiet market, so the line says so outright.
		webStep.Error = err.Error()
		slog.Warn("persona web research failed", "provider", a.search.Name(), "err", err)
	case len(web) == 0:
		// recorded below after optional fallback
	}
	steps = append(steps, webStep)

	// Entity-only retry: "Konu: marka … hepsiburada.com" primary can miss when
	// the long ask confuses a thin scraper; the bare domain usually does not.
	if (err != nil || len(web) == 0) && fallback != "" && fallback != query {
		web2, err2 := a.search.Search(ctx, fallback, maxWebResults)
		fbStep := ResearchStep{Tool: "web_research", Query: fallback, Results: len(web2), Provider: a.search.Name()}
		if err2 != nil {
			fbStep.Error = err2.Error()
			slog.Warn("persona web research fallback failed", "provider", a.search.Name(), "err", err2)
		}
		steps = append(steps, fbStep)
		if err2 == nil && len(web2) > 0 {
			web, err = web2, nil
			query = fallback // wiki follows the query that found pages
		}
	}

	switch {
	case err != nil:
		b.WriteString("- Canlı web araması ÇALIŞMADI (araç hatası). Bu, konu hakkında bilgi olmadığı anlamına gelmez.\n")
	case len(web) == 0:
		b.WriteString("- Canlı web araması sonuç döndürmedi.\n")
	}
	for _, r := range web {
		if r.URL == "" {
			continue
		}
		title := oneLine(r.Title)
		left := room(len(title) + len(r.URL) + citationOverhead)
		if left <= 0 {
			dropped++
			continue
		}
		n++
		sources = append(sources, Source{N: n, Title: r.Title, URL: r.URL, Kind: "web"})
		fmt.Fprintf(&b, "[%d] (web) %s — %s\n%s\n", n, title, r.URL, truncate(r.Snippet, min(maxSnippetLen, left)))
	}

	hits, err := a.wiki.Search(ctx, query, maxWikiResults)
	wikiStep := ResearchStep{Tool: "wiki_retrieve", Query: query, Results: len(hits), Provider: wikiProvider}
	if err != nil {
		// This error used to be assigned and never read: DeepKwiki could be
		// unreachable and the turn carried on as though the corpus were empty.
		wikiStep.Error = err.Error()
		slog.Warn("persona wiki retrieval failed", "err", err)
		b.WriteString("- DeepKwiki araması ÇALIŞMADI (araç hatası). Bilgi tabanı bu tur sorgulanamadı.\n")
	}
	steps = append(steps, wikiStep)
	for _, h := range hits {
		title := h.Title
		if h.Heading != "" {
			title += " · " + h.Heading
		}
		left := room(len(title) + citationOverhead)
		if left <= 0 {
			dropped++
			continue
		}
		n++
		sources = append(sources, Source{N: n, Title: title, URL: h.SourceURL, Kind: "wiki"})
		fmt.Fprintf(&b, "[%d] (DeepKwiki) %s\n%s\n", n, oneLine(title), truncate(h.Body, min(maxSnippetLen, left)))
	}

	if n == 0 {
		// Tool failure ≠ unknown brand. Tell the model to say the search failed,
		// not that Hepsiburada has no public footprint.
		b.WriteString("- Kaynak yok. Konunun var olmadığını söyleme. Kimlik sorusuysa TEK soruyla resmi site/URL iste. Pazarlama/yatırım ise TEK net soru veya varsayımlı geçici cevap. Uydurma kanıt yazma.\n")
	}
	if dropped > 0 {
		// Logged rather than written into the prompt: the model cannot act on
		// evidence it was not given, and saying so in the prompt spends the very
		// budget that ran out. An operator seeing this repeatedly should raise
		// LLM_MAX_PROMPT_TOKENS, and the engine's window with it.
		slog.Info("persona evidence trimmed to fit the prompt budget",
			"budget_chars", budget, "kept", n, "dropped", dropped)
	}
	return sources, b.String(), steps
}

// promptPlan is one turn's character budget, divided up before anything is
// gathered.
type promptPlan struct {
	evidence int // chars the evidence block may occupy
	history  int // chars the prior conversation may occupy
	// latest is the new user message, truncated if it alone exceeds the window.
	latest string
}

// plan divides the budget. The persona prompt, the turn instruction and the new
// user message are what the turn *is* and come out first; what is left is split
// between evidence and history.
func (a *Agent) plan(history []Turn) promptPlan {
	free := a.promptChars - len(personaSystemPrompt) - len(turnInstruction) - promptMarginChars

	latest := history[len(history)-1].Content
	if len(latest) > free {
		// The user's own message outranks every other part of the prompt, but it
		// does not outrank the window. Truncating here is what turns "the engine
		// rejected your message" into "the persona answered the first part of
		// it", which is the better of the two available failures.
		latest = truncate(latest, max(free, 0))
	}
	free -= len(latest)
	if free <= 0 {
		return promptPlan{latest: latest}
	}

	evidence := int(float64(free) * evidenceShare)
	return promptPlan{evidence: evidence, history: free - evidence, latest: latest}
}

// compose builds the message list: the persona, the prior conversation verbatim,
// and a final user turn that carries this turn's evidence plus the instruction.
// The evidence is attached to the user turn rather than the system prompt so it
// stays tied to the question it was gathered for and does not accumulate across
// the conversation.
//
// History is fitted to plan.history newest-first and then replayed in order.
// Dropping the oldest turns rather than the longest is what keeps a clarifying
// question and the answer to it next to each other; losing one half of that pair
// would have the persona ask the same question again.
func (a *Agent) compose(history []Turn, evidence string, plan promptPlan) []llm.Message {
	msgs := make([]llm.Message, 0, len(history)+2)
	msgs = append(msgs, llm.Message{Role: "system", Content: personaSystemPrompt})

	prior := history[:len(history)-1]
	keep, used := 0, 0
	for i := len(prior) - 1; i >= 0; i-- {
		if used+len(prior[i].Content) > plan.history {
			break
		}
		used += len(prior[i].Content)
		keep++
	}
	if keep < len(prior) {
		slog.Info("persona history trimmed to fit the prompt budget",
			"budget_chars", plan.history, "kept", keep, "dropped", len(prior)-keep)
	}
	for _, t := range prior[len(prior)-keep:] {
		role := "user"
		if t.Role == "assistant" {
			role = "assistant"
		}
		msgs = append(msgs, llm.Message{Role: role, Content: t.Content})
	}

	final := evidence + "\n\nKULLANICI: " + plan.latest + "\n\n" + turnInstruction
	msgs = append(msgs, llm.Message{Role: "user", Content: final})
	return msgs
}

// resolveModel follows the same precedence as the analysis and wiki paths.
func (a *Agent) resolveModel(ctx context.Context) string {
	cfg, err := a.settings.Get(ctx)
	if err != nil {
		return defaultModel
	}
	switch {
	case cfg.DefaultModel != "":
		return cfg.DefaultModel
	case cfg.ActiveModelID != "":
		return cfg.ActiveModelID
	default:
		return defaultModel
	}
}

// deriveSubject treats the first user message as the thing under evaluation.
// Later turns refine it, but the subject anchors every research query so a
// one-word answer like a budget figure still researches the right company.
func deriveSubject(history []Turn) string {
	for _, t := range history {
		if t.Role != "assistant" && strings.TrimSpace(t.Content) != "" {
			return strings.TrimSpace(t.Content)
		}
	}
	return ""
}

// buildQuery combines the anchored subject with the latest message so research
// follows the conversation. When the latest message is the subject itself (the
// first turn) it is not repeated.
func buildQuery(subject, latest string) string {
	subject = truncate(subject, 200)
	latest = truncate(strings.TrimSpace(latest), 200)
	if latest == "" || latest == subject {
		return subject
	}
	return subject + " " + latest
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// truncate cuts s to at most n bytes, which is the unit the prompt budget is
// kept in. It backs off to a rune boundary first: the corpus and the persona are
// Turkish, so a cut chosen by arithmetic lands mid-rune often rather than rarely,
// and half a rune reaches the tokeniser as U+FFFD — a byte of budget spent to
// make the surrounding word unreadable.
func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	cut := s[:n]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return strings.TrimSpace(cut) + "…"
}
