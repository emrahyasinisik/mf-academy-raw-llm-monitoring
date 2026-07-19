package common

import (
	"encoding/json"
	"net/http"
)

// JSON writes v as a JSON response with the given status code.
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
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

// Decode parses the JSON request body into dst, rejecting unknown fields so
// typos in client payloads surface as errors instead of being silently dropped.
func Decode(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return ErrBadRequest("invalid JSON body: " + err.Error())
	}
	return nil
}
