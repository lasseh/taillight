package auth

import (
	"regexp"
	"strings"
	"testing"
)

var hexHash = regexp.MustCompile(`^[0-9a-f]{64}$`)

// TestGenerateAPIKey pins the wire contract of API keys: the "tl_" prefix, a
// 43-char base62 body, the display prefix being the first 10 chars, and the
// stored hash matching HashToken(fullKey). A change to apiKeyLen/alphabet that
// broke stored-key lookup (or the prefix[:10] slice) would fail here (audit N2).
func TestGenerateAPIKey(t *testing.T) {
	full, hash, prefix, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey() error = %v", err)
	}

	if !strings.HasPrefix(full, apiKeyPrefix) {
		t.Errorf("key %q lacks prefix %q", full, apiKeyPrefix)
	}
	if want := len(apiKeyPrefix) + apiKeyLen; len(full) != want {
		t.Errorf("len(full) = %d, want %d", len(full), want)
	}
	if prefix != full[:displayPrefixLen] {
		t.Errorf("prefix = %q, want %q", prefix, full[:displayPrefixLen])
	}
	if hash != HashToken(full) {
		t.Errorf("hash does not match HashToken(full)")
	}
	if !hexHash.MatchString(hash) {
		t.Errorf("hash %q is not 64 lowercase hex chars", hash)
	}
	// Body must be drawn from the base62 alphabet only.
	body := strings.TrimPrefix(full, apiKeyPrefix)
	for _, r := range body {
		if !strings.ContainsRune(base62Chars, r) {
			t.Errorf("body contains non-base62 char %q", r)
		}
	}
}

func TestGenerateAPIKey_Unique(t *testing.T) {
	seen := make(map[string]struct{}, 256)
	for range 256 {
		full, _, _, err := GenerateAPIKey()
		if err != nil {
			t.Fatalf("GenerateAPIKey() error = %v", err)
		}
		if _, dup := seen[full]; dup {
			t.Fatalf("duplicate key generated: %q", full)
		}
		seen[full] = struct{}{}
	}
}

func TestHashToken(t *testing.T) {
	h1 := HashToken("tl_example")
	if !hexHash.MatchString(h1) {
		t.Errorf("hash %q is not 64 lowercase hex chars", h1)
	}
	if HashToken("tl_example") != h1 {
		t.Error("HashToken is not deterministic")
	}
	if HashToken("tl_different") == h1 {
		t.Error("distinct inputs produced the same hash")
	}
}

func TestGenerateSessionToken(t *testing.T) {
	raw, hash, err := GenerateSessionToken()
	if err != nil {
		t.Fatalf("GenerateSessionToken() error = %v", err)
	}
	if raw == "" {
		t.Error("raw token is empty")
	}
	if hash != HashToken(raw) {
		t.Error("hash does not match HashToken(raw)")
	}
}
