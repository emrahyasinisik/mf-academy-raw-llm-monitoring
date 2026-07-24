package mcp

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/emrah/mf-backend/internal/common"
	"github.com/go-chi/chi/v5/middleware"
)

// maxBodyBytes bounds one request. MCP messages are small — the largest is a
// case being analysed — and an unbounded read on a network peer is how the peer
// decides how much of our memory it gets.
const maxBodyBytes = 1 << 20 // 1 MiB

// Server speaks MCP over HTTP.
//
// One endpoint taking JSON-RPC by POST, which is the transport every current
// MCP client can use over a network. No SSE stream and no session header: this
// server sends nothing the client did not ask for — no progress notifications,
// no sampling requests, no server-initiated anything — so a stream would be an
// idle connection held open for its own sake.
type Server struct {
	analyzer Analyzer
	name     string
	version  string
}

func NewServer(analyzer Analyzer, name, version string) *Server {
	return &Server{analyzer: analyzer, name: name, version: version}
}

// Handler serves POST /mcp.
//
// Authentication is the same bearer token the rest of the API uses, applied by
// the router before this runs. That matters for more than access control: every
// report is owned by a user, and the MCP client is acting *as* that user rather
// than as an anonymous integration — so a report created through Claude appears
// in the same history as one created in the browser.
func (s *Server) Handler(w http.ResponseWriter, r *http.Request) {
	claims, ok := common.ClaimsFromContext(r.Context())
	if !ok {
		common.Error(w, common.ErrUnauthorized("authentication required"))
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		writeRPC(w, errorResponse(nil, codeParseError, "could not read the request body", nil))
		return
	}

	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		writeRPC(w, errorResponse(nil, codeInvalidRequest, "empty request", nil))
		return
	}

	// A JSON-RPC batch is a top-level array. Refused explicitly rather than
	// failing to decode: batching was removed from MCP in 2025-06-18, and a
	// client sending one deserves to be told that instead of receiving a
	// "cannot unmarshal array" that reads like our bug.
	if trimmed[0] == '[' {
		writeRPC(w, errorResponse(nil, codeInvalidRequest,
			"batched requests are not supported; send one message per request", nil))
		return
	}

	var req request
	if err := json.Unmarshal(trimmed, &req); err != nil {
		writeRPC(w, errorResponse(nil, codeParseError, "invalid JSON", err.Error()))
		return
	}
	if req.JSONRPC != jsonrpcVersion {
		writeRPC(w, errorResponse(req.ID, codeInvalidRequest,
			"jsonrpc must be \"2.0\"", nil))
		return
	}

	resp, send := s.dispatch(r, claims.UserID, req)
	if !send {
		// A notification gets no body at all. Answering one is a protocol
		// violation, and some clients treat an unexpected response to
		// notifications/initialized as a fatal handshake error.
		w.WriteHeader(http.StatusAccepted)
		return
	}
	writeRPC(w, resp)
}

// dispatch routes one message. The bool reports whether a response should be
// written; it is false for notifications.
func (s *Server) dispatch(r *http.Request, userID string, req request) (response, bool) {
	if req.isNotification() {
		// The only notification worth acting on is initialized, and acting on
		// it means nothing here: this server holds no per-session state, so
		// there is no handshake to complete. Logged at debug so an integration
		// problem is still traceable.
		slog.Debug("mcp notification", "method", req.Method)
		return response{}, false
	}

	switch req.Method {
	case "initialize":
		return s.initialize(req), true

	case "ping":
		// Required by the specification and deliberately trivial: an empty
		// result is the whole contract.
		return resultResponse(req.ID, map[string]any{}), true

	case "tools/list":
		return resultResponse(req.ID, toolsListResult{Tools: s.tools()}), true

	case "tools/call":
		return s.toolsCall(r, userID, req), true

	default:
		return errorResponse(req.ID, codeMethodNotFound,
			"unknown method: "+req.Method, nil), true
	}
}

func (s *Server) initialize(req request) response {
	var p initializeParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return errorResponse(req.ID, codeInvalidParams,
				"could not read initialize params", err.Error())
		}
	}

	// Echo the client's version when we speak it, otherwise state ours and let
	// the client decide whether it can proceed. Silently agreeing to a version
	// we do not implement would produce responses the client cannot parse, and
	// the failure would surface far from its cause.
	version := LatestVersion
	if supportsVersion(p.ProtocolVersion) {
		version = p.ProtocolVersion
	} else if p.ProtocolVersion != "" {
		slog.Info("mcp client asked for an unsupported protocol version",
			"requested", p.ProtocolVersion, "offering", version,
			"client", p.ClientInfo.Name)
	}

	slog.Info("mcp initialize",
		"client", p.ClientInfo.Name, "client_version", p.ClientInfo.Version,
		"protocol", version)

	return resultResponse(req.ID, initializeResult{
		ProtocolVersion: version,
		ServerInfo:      serverInfo{Name: s.name, Version: s.version},
		Capabilities:    capabilities{Tools: &toolsCapability{ListChanged: false}},
		Instructions:    instructions,
	})
}

func (s *Server) toolsCall(r *http.Request, userID string, req request) response {
	var p callParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errorResponse(req.ID, codeInvalidParams,
			"could not read tool call params", err.Error())
	}
	if p.Name == "" {
		return errorResponse(req.ID, codeInvalidParams, "name is required", nil)
	}

	started := time.Now()
	result := s.call(r.Context(), userID, p.Name, p.Arguments)

	slog.Info("mcp tool call",
		"request_id", middleware.GetReqID(r.Context()),
		"tool", p.Name, "user_id", userID,
		"is_error", result.IsError,
		"duration_ms", time.Since(started).Milliseconds())

	// Always a successful JSON-RPC response, even when the tool failed. See the
	// note on callResult: an isError result reaches the model, a JSON-RPC error
	// does not.
	return resultResponse(req.ID, result)
}

// writeRPC serialises a response. Always HTTP 200 once the message itself
// parsed — JSON-RPC carries its own error channel, and a client that has to
// read both the status line and the body to learn what happened has two places
// to get it wrong.
func writeRPC(w http.ResponseWriter, resp response) {
	resp.JSONRPC = jsonrpcVersion
	w.Header().Set("Content-Type", "application/json")

	buf, err := json.Marshal(resp)
	if err != nil {
		slog.Error("could not encode an mcp response", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Length", itoa(len(buf)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf)
}

// itoa avoids pulling strconv in for one call site.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
