// Package httputil provides shared HTTP response helpers used across packages
// that cannot import each other (e.g., auth and handler).
package httputil

import (
	"encoding/json"
	"net/http"
)

type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
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
	json.NewEncoder(w).Encode(errorBody{ //nolint:errcheck
		Error: errorDetail{Code: code, Message: msg},
	})
}
