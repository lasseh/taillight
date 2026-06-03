package ldap

import (
	"strings"
	"testing"
)

// TestBuildUserFilter_EscapesInjection exercises the production filter
// construction path (not just the library's EscapeFilter), so removing the
// escaping at the call site would fail the suite (audit N5).
func TestBuildUserFilter_EscapesInjection(t *testing.T) {
	const tmpl = "(uid=%s)"

	got := buildUserFilter(tmpl, "x*)(uid=*)")

	if !strings.Contains(got, `\2a`) {
		t.Errorf("username metacharacters were not escaped: %q", got)
	}
	// None of the raw injection metacharacter sequences may survive.
	for _, raw := range []string{"*)(", ")(uid=", "(uid=*)"} {
		// Allow the single leading template literal "(uid=" only.
		stripped := strings.TrimPrefix(got, "(uid=")
		if strings.Contains(stripped, raw) {
			t.Errorf("LDAP filter injection not neutralised (found %q): %q", raw, got)
		}
	}
	if !strings.HasPrefix(got, "(uid=") || !strings.HasSuffix(got, ")") {
		t.Errorf("template structure altered: %q", got)
	}
}

// TestBuildUserFilter_PlainUsername confirms a benign username renders cleanly.
func TestBuildUserFilter_PlainUsername(t *testing.T) {
	if got := buildUserFilter("(sAMAccountName=%s)", "alice"); got != "(sAMAccountName=alice)" {
		t.Errorf("got %q, want (sAMAccountName=alice)", got)
	}
}
