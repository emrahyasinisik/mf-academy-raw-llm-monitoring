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

// Completion is the answer plus the telemetry a Run needs.
type Completion struct {
	Content          string
	PromptTokens     int
	CompletionTokens int
	LatencyMs        int
}

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

// userAgent identifies this service to the inference host. Deliberately names
// what we are rather than imitating a browser: the point is that whoever reads
// the host's logs can tell which client called.
const userAgent = "mf-backend/0.1.0 (+https://github.com/emrahyasinisik/mf-academy-raw-llm-monitoring)"

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
			Timeout: timeout,
			// The default transport pools connections, which matters here: the
			// upstream is across a tunnel, so a fresh TLS handshake per request
			// would add a round trip to every generation.
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

	maxTokens := req.MaxTokens
	if maxTokens <= 0 || maxTokens > p.maxTokens {
		maxTokens = p.maxTokens
	}

	body, err := json.Marshal(chatRequest{
		Model:       req.Model,
		Messages:    messages,
		Temperature: req.Temperature,
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

	return Completion{
		Content:          parsed.Choices[0].Message.Content,
		PromptTokens:     parsed.Usage.PromptTokens,
		CompletionTokens: parsed.Usage.CompletionTokens,
		LatencyMs:        latencyMs,
	}, nil
}

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
