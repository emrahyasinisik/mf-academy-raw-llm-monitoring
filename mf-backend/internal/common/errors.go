package common

import "net/http"

// APIError is an error that also carries an HTTP status and a stable, machine
// readable code. Handlers return these; the Error() writer turns them into JSON.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string { return e.Message }

// Constructors for the common cases — keeps handler code terse and consistent.

func ErrBadRequest(msg string) *APIError {
	return &APIError{Status: http.StatusBadRequest, Code: "bad_request", Message: msg}
}

func ErrUnauthorized(msg string) *APIError {
	if msg == "" {
		msg = "authentication required"
	}
	return &APIError{Status: http.StatusUnauthorized, Code: "unauthorized", Message: msg}
}

func ErrForbidden(msg string) *APIError {
	return &APIError{Status: http.StatusForbidden, Code: "forbidden", Message: msg}
}

func ErrNotFound(msg string) *APIError {
	if msg == "" {
		msg = "resource not found"
	}
	return &APIError{Status: http.StatusNotFound, Code: "not_found", Message: msg}
}

func ErrConflict(msg string) *APIError {
	return &APIError{Status: http.StatusConflict, Code: "conflict", Message: msg}
}

func ErrTooManyRequests(msg string) *APIError {
	if msg == "" {
		msg = "too many requests"
	}
	return &APIError{Status: http.StatusTooManyRequests, Code: "too_many_requests", Message: msg}
}

func ErrInternal(msg string) *APIError {
	return &APIError{Status: http.StatusInternalServerError, Code: "internal_error", Message: msg}
}

// ErrUnavailable reports that a dependency this request needed could not be
// reached at all. Kept distinct from ErrUpstreamTimeout because the operator
// response differs: this one means the machine is off, asleep or the tunnel is
// down, and no amount of waiting will help.
func ErrUnavailable(msg string) *APIError {
	if msg == "" {
		msg = "a required service is unavailable"
	}
	return &APIError{Status: http.StatusServiceUnavailable, Code: "unavailable", Message: msg}
}

// ErrUpstreamTimeout reports that a dependency was reachable but did not answer
// in time — the request was accepted and is presumably still being worked on,
// which is a different situation from the service being down.
func ErrUpstreamTimeout(msg string) *APIError {
	if msg == "" {
		msg = "upstream service timed out"
	}
	return &APIError{Status: http.StatusGatewayTimeout, Code: "upstream_timeout", Message: msg}
}

// ErrUpstreamFailed reports that a dependency answered, but with a failure. The
// message is written by us, never copied from the upstream body — that body can
// carry internal detail a client has no business seeing.
func ErrUpstreamFailed(msg string) *APIError {
	if msg == "" {
		msg = "upstream service failed"
	}
	return &APIError{Status: http.StatusBadGateway, Code: "upstream_failed", Message: msg}
}
