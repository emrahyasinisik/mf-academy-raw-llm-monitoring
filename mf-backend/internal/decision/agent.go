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
	"strings"

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
}

func NewAgent(chat Chatter, search Searcher, retriever WikiRetriever, set SettingsSource) *Agent {
	return &Agent{chat: chat, search: search, wiki: retriever, settings: set}
}

// defaultModel matches the analysis and wiki paths: the fallback when neither
// the operator's settings name a model nor an adapter is active.
const defaultModel = "gemma-2-2b-it-q4f16_1-MLC"

// personaTemperature keeps the verdict grounded. This is an analyst, not a
// brainstorm; a low temperature is what stops it inventing evidence.
const personaTemperature = 0.2

// personaMaxTokens bounds one reply. Generous enough for a full verdict with a
// per-dimension rationale, bounded so a single turn cannot occupy the GPU.
const personaMaxTokens = 1024

// Bounds on how much evidence goes into one prompt. The card has 6 GB; an
// unbounded evidence block is the fastest way to run the KV cache out of it.
const (
	maxWebResults  = 5
	maxWikiResults = 4
	maxSnippetLen  = 500
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
type ResearchStep struct {
	Tool    string `json:"tool"`
	Query   string `json:"query"`
	Results int    `json:"results"`
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

	subject := deriveSubject(history)
	query := buildQuery(subject, latest.Content)

	sources, evidence, steps := a.gather(ctx, query)

	model := a.resolveModel(ctx)
	messages := a.compose(history, evidence)

	comp, err := a.chat.Chat(ctx, model, messages, personaTemperature, personaMaxTokens)
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
func (a *Agent) gather(ctx context.Context, query string) ([]Source, string, []ResearchStep) {
	var (
		sources []Source
		b       strings.Builder
		steps   []ResearchStep
		n       int
	)

	b.WriteString(evidenceHeader + "\n")

	web, err := a.search.Search(ctx, query, maxWebResults)
	steps = append(steps, ResearchStep{Tool: "web_research", Query: query, Results: len(web)})
	if err != nil || len(web) == 0 {
		b.WriteString("- Canlı web araması sonuç döndürmedi.\n")
	}
	for _, r := range web {
		if r.URL == "" {
			continue
		}
		n++
		sources = append(sources, Source{N: n, Title: r.Title, URL: r.URL, Kind: "web"})
		fmt.Fprintf(&b, "[%d] (web) %s — %s\n%s\n", n, oneLine(r.Title), r.URL, truncate(r.Snippet, maxSnippetLen))
	}

	hits, err := a.wiki.Search(ctx, query, maxWikiResults)
	steps = append(steps, ResearchStep{Tool: "wiki_retrieve", Query: query, Results: len(hits)})
	for _, h := range hits {
		n++
		title := h.Title
		if h.Heading != "" {
			title += " · " + h.Heading
		}
		sources = append(sources, Source{N: n, Title: title, URL: h.SourceURL, Kind: "wiki"})
		fmt.Fprintf(&b, "[%d] (DeepKwiki) %s\n%s\n", n, oneLine(title), truncate(h.Body, maxSnippetLen))
	}

	if n == 0 {
		b.WriteString("- Hiçbir kaynak bulunamadı; kullanıcıdan daha fazla bilgi iste veya kararının belirsiz olduğunu söyle.\n")
	}
	return sources, b.String(), steps
}

// compose builds the message list: the persona, the prior conversation verbatim,
// and a final user turn that carries this turn's evidence plus the instruction.
// The evidence is attached to the user turn rather than the system prompt so it
// stays tied to the question it was gathered for and does not accumulate across
// the conversation.
func (a *Agent) compose(history []Turn, evidence string) []llm.Message {
	msgs := make([]llm.Message, 0, len(history)+2)
	msgs = append(msgs, llm.Message{Role: "system", Content: personaSystemPrompt})

	for _, t := range history[:len(history)-1] {
		role := "user"
		if t.Role == "assistant" {
			role = "assistant"
		}
		msgs = append(msgs, llm.Message{Role: role, Content: t.Content})
	}

	latest := history[len(history)-1].Content
	final := evidence + "\n\nKULLANICI: " + latest + "\n\n" + turnInstruction
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

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return strings.TrimSpace(s[:n]) + "…"
}
