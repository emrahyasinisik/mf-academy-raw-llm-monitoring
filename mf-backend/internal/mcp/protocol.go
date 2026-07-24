// Package mcp exposes the analysis engine over the Model Context Protocol, so
// an MCP client — Claude, Cursor, an agent someone else wrote — can run a
// rubric analysis without going through this project's own UI.
//
// Hand-rolled rather than built on an SDK. The surface actually needed here is
// four methods and a tool list; an SDK would bring a dependency, its own
// lifecycle model and its own transport abstractions to wrap something this
// small, and the protocol is plain JSON-RPC 2.0 with a handful of MCP-specific
// message shapes on top. The trade is that version negotiation and error
// mapping are ours to get right, which is why both are explicit below rather
// than incidental.
package mcp

import (
	"encoding/json"
	"fmt"
)

// Protocol versions this server understands, newest first.
//
// MCP versions its wire format by date and a client states which it wants
// during initialize. Negotiation is a real requirement rather than a
// formality: a client that asks for a version we do not know must be told what
// we do speak, not handed a response it cannot parse. Answering with our
// newest when we do not recognise the request is what the specification calls
// for — the client then decides whether it can proceed.
var supportedVersions = []string{
	"2025-06-18",
	"2025-03-26",
	"2024-11-05",
}

// LatestVersion is what we advertise when a client asks for something unknown.
const LatestVersion = "2025-06-18"

func supportsVersion(v string) bool {
	for _, s := range supportedVersions {
		if s == v {
			return true
		}
	}
	return false
}

// ---- JSON-RPC 2.0 ----

// jsonrpcVersion is the only value the version field may take. A request
// carrying anything else is not a JSON-RPC message and is refused rather than
// interpreted charitably.
const jsonrpcVersion = "2.0"

// request is one incoming call.
//
// ID is json.RawMessage because JSON-RPC permits a string, a number or null,
// and the response must echo the value back *unchanged*. Decoding it into a
// typed field would turn a client's string id into a number or lose precision
// on a large integer, and the client would then fail to match the response to
// its call.
//
// A nil ID marks a notification: no response is sent at all, which matters for
// notifications/initialized — answering it is a protocol violation that some
// clients treat as fatal.
type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

func (r request) isNotification() bool {
	return len(r.ID) == 0
}

// response is one outgoing reply. Result and Error are mutually exclusive, and
// both are pointers so exactly one is serialised.
type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// rpcError is a protocol-level failure: malformed JSON, an unknown method,
// bad parameters.
//
// Deliberately NOT how a tool reports that its work failed. A tool that ran
// correctly and found the inference host switched off has not violated the
// protocol — it has a result, and that result is bad news. Reporting it as a
// JSON-RPC error tells the client's model nothing it can act on, because the
// error never reaches the model at all; it is swallowed by the transport layer.
// Tool failures travel as isError on a normal result. See callResult.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Standard JSON-RPC 2.0 codes.
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternalError  = -32603
)

func errorResponse(id json.RawMessage, code int, msg string, data any) response {
	return response{
		JSONRPC: jsonrpcVersion,
		ID:      id,
		Error:   &rpcError{Code: code, Message: msg, Data: data},
	}
}

func resultResponse(id json.RawMessage, result any) response {
	return response{JSONRPC: jsonrpcVersion, ID: id, Result: result}
}

// ---- MCP message shapes ----

// initializeParams is what a client sends first.
type initializeParams struct {
	ProtocolVersion string `json:"protocolVersion"`
	ClientInfo      struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"clientInfo"`
	Capabilities map[string]any `json:"capabilities"`
}

// initializeResult announces what this server is and what it can do.
type initializeResult struct {
	ProtocolVersion string       `json:"protocolVersion"`
	ServerInfo      serverInfo   `json:"serverInfo"`
	Capabilities    capabilities `json:"capabilities"`
	// Instructions is shown to the client's model as context about the server.
	// Worth writing carefully: it is the only place to say that a report's
	// score is meaningless without its coverage, and a model that does not know
	// that will quote the number alone.
	Instructions string `json:"instructions,omitempty"`
}

type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// capabilities advertises only tools. No resources, no prompts, no sampling —
// declaring a capability this server does not implement invites clients to call
// it, and an empty object is the honest answer.
type capabilities struct {
	Tools *toolsCapability `json:"tools,omitempty"`
}

type toolsCapability struct {
	// ListChanged reports whether the server pushes a notification when its
	// tool list changes. It does not: the tools are fixed at compile time.
	ListChanged bool `json:"listChanged"`
}

// Tool is one callable operation.
type Tool struct {
	Name        string         `json:"name"`
	Title       string         `json:"title,omitempty"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type toolsListResult struct {
	Tools []Tool `json:"tools"`
}

type callParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// content is one block of a tool's answer.
type content struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// callResult is what a tool returns.
//
// IsError is the tool-level failure channel, distinct from rpcError above. The
// distinction is not pedantry: a JSON-RPC error is handled by the client's
// transport and never reaches the model, while an isError result is delivered
// to the model as text it can read and respond to. "The inference host is
// switched off" is something a model should be told; "your JSON was malformed"
// is not.
type callResult struct {
	Content []content `json:"content"`
	IsError bool      `json:"isError,omitempty"`
}

// textResult wraps a value as a tool's successful answer.
//
// Structured data is serialised into a text block rather than sent as a
// structuredContent field. Text is understood by every protocol version this
// server supports, whereas structured output arrived later; sending JSON as
// text costs nothing a model cares about and cannot be misparsed by an older
// client.
func textResult(v any) callResult {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return errorResult(fmt.Sprintf("could not encode the result: %v", err))
	}
	return callResult{Content: []content{{Type: "text", Text: string(b)}}}
}

func errorResult(msg string) callResult {
	return callResult{
		Content: []content{{Type: "text", Text: msg}},
		IsError: true,
	}
}
