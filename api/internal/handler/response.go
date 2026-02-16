// Package handler provides HTTP handlers for the syslog SSE API.
package handler

import (
	"encoding/json"
	"net/http"

	"github.com/lasseh/taillight/internal/httputil"
)

// listResponse wraps a slice result with a data envelope.
type listResponse struct {
	Data    any     `json:"data"`
	Cursor  *string `json:"cursor,omitempty"`
	HasMore bool    `json:"has_more"`
}

// itemResponse wraps a single item with a data envelope.
type itemResponse struct {
	Data any `json:"data"`
}

// errorBody is the structured error envelope used by tests to unmarshal
// error responses. The actual encoding is handled by httputil.WriteError.
type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

// writeJSONStatus sends a JSON response with a specific HTTP status code.
// Content-Type is set before WriteHeader to ensure it appears in the response.
func writeJSONStatus(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

// writeError sends a JSON error response. Content-Type must be set before
// WriteHeader because WriteHeader locks the header map.
func writeError(w http.ResponseWriter, status int, code, msg string) {
	httputil.WriteError(w, status, code, msg)
}

// mustJSON marshals v to JSON. Returns nil, false on error.
// Callers should handle the error case (e.g., skip the event).
func mustJSON(v any) ([]byte, bool) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, false
	}
	return data, true
}

// emptySlice ensures nil slices are returned as [] in JSON.
func emptySlice[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
