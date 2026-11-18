package worker

import (
	"fmt"
	"strings"
	"testing"

	"github.com/lasseh/taillight/internal/analyzer"
	"github.com/lasseh/taillight/internal/ollama"
)

// TestSanitizeRunErrRejectedKey covers the message stored on the report row
// when the backend refuses the key: it names the setting to fix, and carries
// neither the upstream body nor anything a server might echo into it.
func TestSanitizeRunErrRejectedKey(t *testing.T) {
	err := fmt.Errorf("%w: %w", analyzer.ErrUnauthorized, &ollama.StatusError{
		StatusCode: 401,
		Body:       `{"error":{"message":"Authentication Error, Invalid API key provided: sk-echoed-by-the-server","type":"token_not_found_in_db"}}`,
	})

	got := sanitizeRunErr(err)
	if want := "analysis backend rejected the API key; check analysis.api_key"; got != want {
		t.Errorf("sanitizeRunErr() = %q, want %q", got, want)
	}
	if strings.Contains(got, "sk-echoed") || strings.Contains(got, "token_not_found") {
		t.Errorf("sanitized message carries the upstream body: %q", got)
	}
}
