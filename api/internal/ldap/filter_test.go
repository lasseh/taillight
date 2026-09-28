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

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		filter  string
		wantErr bool
	}{
		{"(&(objectClass=person)(uid=%s))", false},
		{"(sAMAccountName=%s)", false},
		{"(cn=100%%-%s)", false},
		{"(uid=alice)", true},
		{"(|(uid=%s)(mail=%s))", true},
		{"(uid=%d)", true},
		{"", true},
	}
	for _, tt := range tests {
		err := Config{URL: "ldaps://x", UserFilter: tt.filter}.Validate()
		if (err != nil) != tt.wantErr {
			t.Errorf("Validate(%q) = %v, wantErr %v", tt.filter, err, tt.wantErr)
		}
	}
	if err := (Config{UserFilter: "(uid=%s)"}).Validate(); err == nil {
		t.Error("Validate with no URL should fail")
	}
}
