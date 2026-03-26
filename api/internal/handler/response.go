// Package handler provides HTTP handlers for the srvlog SSE API.
package handler

import (
	"encoding/json"
	"log/slog"
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

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Default().Error("writeJSON encode failed", "err", err)
	}
}

// writeJSONStatus sends a JSON response with a specific HTTP status code.
// Content-Type is set before WriteHeader to ensure it appears in the response.
func writeJSONStatus(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Default().Error("writeJSONStatus encode failed", "err", err)
	}
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

// isClientGone returns true if the request context was canceled (client
// disconnected). Callers should return early without logging — there is
// no one to send a response to.
func isClientGone(r *http.Request) bool {
	return r.Context().Err() != nil
}

// emptySlice ensures nil slices are returned as [] in JSON.
func emptySlice[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
