package ldap

import (
	"strings"
	"testing"

	ldaplib "github.com/go-ldap/ldap/v3"
)

func TestIsAccountLocked(t *testing.T) {
	tests := []struct {
		name   string
		value  string
		locked bool
	}{
		{"TRUE uppercase", "TRUE", true},
		{"true lowercase", "true", true},
		{"True mixed", "True", true},
		{"FALSE", "FALSE", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := &ldaplib.Entry{}
			if tt.value != "" {
				entry.Attributes = []*ldaplib.EntryAttribute{
					{Name: "nsAccountLock", Values: []string{tt.value}},
				}
			}
			if got := isAccountLocked(entry); got != tt.locked {
				t.Errorf("isAccountLocked() = %v, want %v", got, tt.locked)
			}
		})
	}
}

func TestCheckAdminGroup(t *testing.T) {
	adminDN := "cn=admins,cn=groups,cn=accounts,dc=example,dc=com"

	tests := []struct {
		name       string
		adminGroup string
		memberOf   []string
		want       bool
	}{
		{
			"member of admin group",
			adminDN,
			[]string{"cn=users,cn=groups,cn=accounts,dc=example,dc=com", adminDN},
			true,
		},
		{
			"not member",
			adminDN,
			[]string{"cn=users,cn=groups,cn=accounts,dc=example,dc=com"},
			false,
		},
		{
			"case insensitive match",
			adminDN,
			[]string{"CN=admins,CN=groups,CN=accounts,DC=example,DC=com"},
			true,
		},
		{
			"no admin group configured",
			"",
			[]string{adminDN},
			false,
		},
		{
			"empty memberOf",
			adminDN,
			nil,
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Client{cfg: Config{AdminGroup: tt.adminGroup}}
			entry := &ldaplib.Entry{}
			if len(tt.memberOf) > 0 {
				entry.Attributes = []*ldaplib.EntryAttribute{
					{Name: "memberOf", Values: tt.memberOf},
				}
			}
			if got := c.checkAdminGroup(entry); got != tt.want {
				t.Errorf("checkAdminGroup() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEscapeFilterInUserSearch(t *testing.T) {
	// Verify that special LDAP filter characters are escaped.
	dangerous := `admin)(uid=*)`
	escaped := ldaplib.EscapeFilter(dangerous)
	if escaped == dangerous {
		t.Fatal("EscapeFilter did not escape dangerous input")
	}
	// Parentheses and asterisks must be escaped.
	for _, ch := range []string{")(", "(uid", "*)"} {
		if strings.Contains(escaped, ch) {
			t.Errorf("escaped filter still contains %q: %s", ch, escaped)
		}
	}
}
