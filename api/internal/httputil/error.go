// Package httputil provides shared HTTP response helpers used across packages
// that cannot import each other (e.g., auth and handler).
package httputil

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// ErrorBody is the structured error envelope for JSON error responses.
type ErrorBody struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail contains the code and message for an error response.
type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// WriteError sends a JSON error response with the standard envelope:
//
//	{"error":{"code":"...","message":"..."}}
//
// Content-Type is set before WriteHeader to ensure it appears in the response.
func WriteError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(ErrorBody{
		Error: ErrorDetail{Code: code, Message: msg},
	}); err != nil {
		slog.Default().Error("WriteError encode failed", "err", err)
	}
}
