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
