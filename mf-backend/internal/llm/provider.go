package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/emrah/mf-backend/internal/common"
)

// This file is the server-side half of the LLM. The browser half (WebLLM on
// WebGPU) is unchanged and still posts its own results to POST /llm/runs; this
// path exists so the same model id can also be executed on hardware we control,
// which is what makes latency comparable between runs. Measured in a browser,
// latency describes the visitor's GPU more than it describes the model.
//
// The wire format is the OpenAI chat-completions dialect because that is what
// `mlc_llm serve` speaks. Deliberately: it means the same client also works
// against a hosted provider, so moving inference elsewhere is a change of
// LLM_BASE_URL rather than a change of code.

// CompletionRequest is one generation, independent of who serves it.
type CompletionRequest struct {
	Model        string
	Prompt       string
	SystemPrompt string
	Temperature  float64
	MaxTokens    int
}

// Message is one turn of a conversation, in the OpenAI dialect ("system",
// "user", "assistant"). The multi-turn Chat path carries these directly; the
// single-shot Generate path builds a two-message slice from a system prompt and
// a user prompt and never exposes this type.
type Message struct {
	Role    string
	Content string
}

// Completion is the answer plus the telemetry a Run needs.
type Completion struct {
	Content          string
	PromptTokens     int
	CompletionTokens int
	LatencyMs        int
	// Truncated reports that generation stopped at the token limit rather than
	// because the model was finished. Callers that parse structured output must
	// check it: a cut-off answer is indistinguishable from a malformed one by
	// inspection, and blaming the model for a budget we set is how a bad
	// baseline gets recorded as fact.
	Truncated bool
}

// finishLength is the OpenAI-dialect finish_reason meaning the token limit was
// reached. mlc_llm serve speaks the same dialect.
const finishLength = "length"

// OpenAIProvider calls an OpenAI-compatible /v1/chat/completions endpoint.
type OpenAIProvider struct {
	baseURL   string
	apiKey    string
	maxTokens int
	client    *http.Client
}

// defaultMaxTokens bounds generation. Without a ceiling a single verbose answer
// can run until the request deadline, and on a 6 GB card that deadline is the
// only thing that would ever stop it. A bounded response is also a bounded
// database row and a bounded JSON payload.
const defaultMaxTokens = 512

// maxResponseBytes caps what we read back. The upstream is trusted-ish but it
// is still a network peer, and an unbounded io.ReadAll on a response body is
// how a peer decides how much of our memory it gets to use.
const maxResponseBytes = 4 << 20 // 4 MiB

// userAgent identifies this service to the inference host. Shared with every
// other caller that crosses the same tunnel — see common.UserAgent for why it
// is not defined here.
const userAgent = common.UserAgent

// NewOpenAIProvider builds a provider. A zero timeout leaves the bound entirely
// to the request context; the caller is expected to set one.
func NewOpenAIProvider(baseURL, apiKey string, timeout time.Duration, maxTokens int) *OpenAIProvider {
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}
	return &OpenAIProvider{
		baseURL:   strings.TrimRight(baseURL, "/"),
		apiKey:    apiKey,
		maxTokens: maxTokens,
		client: &http.Client{
			// Deadlines come from the request context on each call. A fixed
			// client.Timeout fought nested routes — a persona turn that spent
			// 20s on search then started inference could hit the client's wall
			// clock even when the route still had budget left.
			Timeout:   0,
			Transport: http.DefaultTransport,
		},
	}
}

// Configured reports whether a provider was wired at all. The server runs fine
// without one — the browser path does not need it — so the endpoint checks this
// and answers honestly rather than the process refusing to boot.
func (p *OpenAIProvider) Configured() bool { return p != nil && p.baseURL != "" }

// ---- wire types ----

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens"`
	Stream      bool          `json:"stream"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
		// FinishReason is "stop" when the model ended on its own and "length"
		// when it hit the token limit. Decoded because the difference is not
		// otherwise visible: a truncated answer is a well-formed HTTP 200 whose
		// body simply stops, and a caller parsing it sees malformed content
		// with no way to tell whether the model or the limit was at fault.
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// Generate runs one prompt against the configured endpoint.
//
// The returned latency is measured around the HTTP round trip, so it includes
// the network hop to the inference host. That is the honest number for a
// user-facing metric — it is what the caller waited — but it means the figure is
// not directly comparable to a browser run, which has no network in it. Runs are
// tagged with their target so the two are never averaged together blindly.
func (p *OpenAIProvider) Generate(ctx context.Context, req CompletionRequest) (Completion, error) {
	if !p.Configured() {
		return Completion{}, common.ErrUnavailable("server-side inference is not configured")
	}

	messages := make([]chatMessage, 0, 2)
	if s := strings.TrimSpace(req.SystemPrompt); s != "" {
		messages = append(messages, chatMessage{Role: "system", Content: s})
	}
	messages = append(messages, chatMessage{Role: "user", Content: req.Prompt})

	return p.dispatch(ctx, req.Model, messages, req.Temperature, req.MaxTokens)
}

// Chat runs a multi-turn conversation against the configured endpoint. It is the
// single-shot Generate's sibling: same wire format, same clamping, same error
// handling — the only difference is that the caller supplies the whole message
// history rather than one system/user pair. The backend-orchestrated decision
// agent uses this to carry a conversation plus the evidence it has gathered.
func (p *OpenAIProvider) Chat(ctx context.Context, model string, messages []Message, temperature float64, maxTokens int) (Completion, error) {
	if !p.Configured() {
		return Completion{}, common.ErrUnavailable("server-side inference is not configured")
	}
	wire := make([]chatMessage, 0, len(messages))
	for _, m := range messages {
		if strings.TrimSpace(m.Content) == "" {
			continue
		}
		wire = append(wire, chatMessage{Role: m.Role, Content: m.Content})
	}
	if len(wire) == 0 {
		return Completion{}, common.ErrBadRequest("no messages to send")
	}
	return p.dispatch(ctx, model, wire, temperature, maxTokens)
}

// dispatch is the shared HTTP round trip both Generate and Chat funnel through,
// so the token clamp, headers, error classification and truncation handling are
// written once and cannot drift apart between the two paths.
func (p *OpenAIProvider) dispatch(ctx context.Context, model string, messages []chatMessage, temperature float64, wantTokens int) (Completion, error) {
	// Three cases, distinguished deliberately. Collapsing them into one clamp
	// cost a whole measurement run: the analysis path computes the budget its
	// rubric needs, that number was silently reduced to the chat default, every
	// answer stopped mid-JSON, and the conclusion drawn was "the model cannot
	// hold the schema" when the truth was "we cut it off".
	maxTokens := wantTokens
	switch {
	case maxTokens <= 0:
		// No opinion from the caller: a modest default, since most callers are
		// conversational and a long answer costs GPU time nobody asked for.
		maxTokens = defaultMaxTokens
	case maxTokens > p.maxTokens:
		// A real ceiling, so a caller cannot occupy the card indefinitely — but
		// never again silently. Anything that asked for more than it got has to
		// be able to find out why from the logs.
		slog.Warn("requested max_tokens exceeds the configured ceiling; clamping",
			"requested", maxTokens, "ceiling", p.maxTokens, "model", model)
		maxTokens = p.maxTokens
	}

	body, err := json.Marshal(chatRequest{
		Model:       model,
		Messages:    messages,
		Temperature: temperature,
		MaxTokens:   maxTokens,
		Stream:      false,
	})
	if err != nil {
		return Completion{}, common.ErrInternal("could not encode inference request")
	}

	url := p.baseURL + "/v1/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return Completion{}, common.ErrInternal("could not build inference request")
	}
	httpReq.Header.Set("Content-Type", "application/json")
	// Identify ourselves honestly. Go's default is "Go-http-client/2.0", which
	// tells an operator reading the inference host's logs nothing about who
	// called, and is the kind of anonymous client that edge protections in front
	// of the tunnel treat as suspect.
	httpReq.Header.Set("User-Agent", userAgent)
	if p.apiKey != "" {
		// Two headers for one secret: the Caddy gateway in front of mlc_llm
		// checks X-API-Key, while hosted OpenAI-compatible providers check the
		// bearer token. Sending both is what keeps LLM_BASE_URL swappable
		// without a code change.
		httpReq.Header.Set("X-API-Key", p.apiKey)
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	started := time.Now()
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return Completion{}, classifyTransportError(ctx, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return Completion{}, classifyTransportError(ctx, err)
	}
	latencyMs := int(time.Since(started).Milliseconds())

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Logged in full for us, summarised for the client. A 401 here means the
		// gateway rejected our key, which is a deployment fault, not a user one.
		slog.Error("inference upstream returned an error",
			"status", resp.StatusCode,
			"body", truncate(string(raw), 512),
		)
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return Completion{}, common.ErrUpstreamFailed("inference host rejected this service's credentials")
		}
		if resp.StatusCode == http.StatusBadRequest {
			// A 400 from the engine is always about the request we built, never
			// about the user's input: an over-long prompt, an unknown field, a
			// sampling value out of range. The engine states which, and passing
			// that through is the difference between one line and an afternoon —
			// "prompt has 2331 tokens, larger than the model input length limit
			// 1366" is a diagnosis, "inference host returned 400" is a puzzle.
			if msg := upstreamMessage(raw); msg != "" {
				return Completion{}, common.ErrUpstreamFailed("inference host rejected the request: " + msg)
			}
		}
		return Completion{}, common.ErrUpstreamFailed(
			fmt.Sprintf("inference host returned %d", resp.StatusCode))
	}

	var parsed chatResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		slog.Error("could not decode inference response", "error", err, "body", truncate(string(raw), 512))
		return Completion{}, common.ErrUpstreamFailed("inference host returned an unreadable response")
	}
	if len(parsed.Choices) == 0 {
		return Completion{}, common.ErrUpstreamFailed("inference host returned no choices")
	}

	finish := parsed.Choices[0].FinishReason
	if finish == finishLength {
		// Worth a log line of its own: a truncated answer is a 200 whose body
		// merely stops, so without this the only symptom is a downstream parse
		// failure that looks like the model's fault rather than the budget's.
		slog.Warn("inference answer was cut off by the token limit",
			"model", model, "max_tokens", maxTokens,
			"completion_tokens", parsed.Usage.CompletionTokens)
	}

	return Completion{
		Content:          stripReasoning(parsed.Choices[0].Message.Content, model),
		PromptTokens:     parsed.Usage.PromptTokens,
		CompletionTokens: parsed.Usage.CompletionTokens,
		LatencyMs:        latencyMs,
		Truncated:        finish == finishLength,
	}, nil
}

// stripReasoning removes a Qwen-style <think> block from the front of an answer.
//
// Here rather than in each caller because it is a property of the served model,
// not of the feature asking. The host now serves a Qwen3 build, and Qwen3 opens
// every reply with a <think> block — empty when the chat template does not enable
// thinking, which is the case for this stack, but still present in the text. Left
// in, it is not merely noise: the rubric and wiki paths parse the answer as JSON
// and a leading block makes every one of them fail to parse, while the persona
// renders it to the user as part of the verdict.
//
// Only a *leading* block is removed, and only a closed one. An unterminated
// <think> means the token budget was spent reasoning and the answer never
// arrived; the raw text is returned in that case, because a caller can act on a
// visibly reasoning-only reply and cannot act on an empty string.
func stripReasoning(content, model string) string {
	trimmed := strings.TrimLeft(content, " \t\r\n")
	if !strings.HasPrefix(trimmed, thinkOpen) {
		return content
	}
	end := strings.Index(trimmed, thinkClose)
	if end < 0 {
		slog.Warn("answer is an unterminated reasoning block; returning it raw",
			"model", model, "chars", len(content))
		return content
	}
	return strings.TrimSpace(trimmed[end+len(thinkClose):])
}

const (
	thinkOpen  = "<think>"
	thinkClose = "</think>"
)

// upstreamMessage digs the engine's own explanation out of an error body, or
// returns "" when there is nothing quotable in there.
//
// Three shapes, because the two engines behind the gateway do not agree and one
// of them does not agree with itself. OpenAI (and llama.cpp) nest the text under
// "error"; mlc_llm puts it at the top level as {"object":"error","message":…};
// and mlc_llm actually sends that object *JSON-encoded as a string*, so the body
// has to be unwrapped once before it parses. Guessing wrong on any of these is
// not harmful — the caller falls back to the bare status code.
func upstreamMessage(raw []byte) string {
	var shape struct {
		Message string `json:"message"`
		Error   struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	for attempt := 0; attempt < 2; attempt++ {
		if err := json.Unmarshal(raw, &shape); err == nil {
			if m := strings.TrimSpace(shape.Error.Message); m != "" {
				return truncate(m, maxUpstreamMessage)
			}
			return truncate(strings.TrimSpace(shape.Message), maxUpstreamMessage)
		}
		// Not an object: possibly the doubly-encoded case. Unwrap the string
		// layer once and try again; a second failure ends the loop.
		var nested string
		if err := json.Unmarshal(raw, &nested); err != nil {
			return ""
		}
		raw = []byte(nested)
	}
	return ""
}

// maxUpstreamMessage bounds what a remote host can print into our error
// response. The engine is ours, but an error body is still untrusted length.
const maxUpstreamMessage = 300

// classifyTransportError separates "did not answer in time" from "could not be
// reached", because the operator does different things about each. The
// inference host is a desktop machine behind a tunnel: asleep is a genuinely
// common state, and it should not look like a server bug.
func classifyTransportError(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.Is(ctx.Err(), context.DeadlineExceeded):
		return common.ErrUpstreamTimeout("inference host did not answer in time")
	case errors.Is(err, context.Canceled), errors.Is(ctx.Err(), context.Canceled):
		// The caller hung up. Nothing to report upstream.
		return common.ErrUpstreamTimeout("request cancelled before inference finished")
	default:
		slog.Error("inference host unreachable", "error", err)
		return common.ErrUnavailable("inference host is unreachable")
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
