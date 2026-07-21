package common

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
)

// bufPool recycles the scratch buffers used to render responses. Response
// encoding happens on every request, so the buffers are the single most
// frequently allocated object in the service.
var bufPool = sync.Pool{New: func() any { return new(bytes.Buffer) }}

// maxPooledBuffer bounds what goes back into the pool. Returning an arbitrarily
// grown buffer would let one huge response permanently pin that much memory.
const maxPooledBuffer = 64 << 10

// JSON writes v as a JSON response with the given status code.
//
// The value is serialized into a pooled buffer before anything is written to
// the wire. That ordering matters: encoding straight into the ResponseWriter
// commits the status code first, so a mid-encode failure would leave the client
// holding a truncated body under a 200. Buffering also yields a Content-Length,
// which keeps small responses out of chunked encoding.
func JSON(w http.ResponseWriter, status int, v any) {
	if v == nil {
		w.WriteHeader(status)
		return
	}

	buf := bufPool.Get().(*bytes.Buffer)
	defer func() {
		buf.Reset()
		if buf.Cap() <= maxPooledBuffer {
			bufPool.Put(buf)
		}
	}()

	if err := json.NewEncoder(buf).Encode(v); err != nil {
		// Nothing has been written yet, so a clean error response is still possible.
		slog.Error("json encode failed", "error", err)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"internal_error","message":"could not encode response"}`))
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}

// ErrorResponse is the single, consistent error shape the API returns.
// A predictable envelope lets the frontend handle every failure the same way.
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

// Error writes a typed APIError (or a generic 500) as JSON.
func Error(w http.ResponseWriter, err error) {
	if apiErr, ok := err.(*APIError); ok {
		JSON(w, apiErr.Status, ErrorResponse{Error: apiErr.Code, Message: apiErr.Message})
		return
	}
	JSON(w, http.StatusInternalServerError, ErrorResponse{Error: "internal_error", Message: "something went wrong"})
}

// maxBodyBytes caps request payloads. Prompts and responses are the largest
// legitimate fields and sit far below this; the limit exists so a single caller
// cannot drive the process out of memory with an unbounded body.
const maxBodyBytes = 1 << 20 // 1 MiB

// Decode parses the JSON request body into dst, rejecting unknown fields so
// typos in client payloads surface as errors instead of being silently dropped.
// The body is size-capped here rather than at each call site: a bound every
// caller must remember is a bound that eventually gets forgotten.
func Decode(r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, maxBodyBytes)

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return ErrBadRequest("request body too large")
		}
		return ErrBadRequest("invalid JSON body: " + err.Error())
	}
	// Reject trailing data so a body like `{...}{...}` fails loudly instead of
	// having its second half silently ignored.
	if dec.More() {
		return ErrBadRequest("unexpected data after JSON body")
	}
	return nil
}
