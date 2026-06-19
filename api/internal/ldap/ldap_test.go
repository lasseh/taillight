package ldap

import (
	"strings"
	"testing"

	ldaplib "github.com/go-ldap/ldap/v3"
)

func TestCnOf(t *testing.T) {
	tests := map[string]string{
		"CN=taillight-admin,OU=NetOps,DC=vegvesen,DC=no": "taillight-admin",
		"cn=admins,dc=example,dc=com":                    "admins",
		"taillight-user":                                 "taillight-user",
		"OU=foo,DC=x":                                    "OU=foo", // first RDN is not a CN
	}
	for dn, want := range tests {
		if got := cnOf(dn); got != want {
			t.Errorf("cnOf(%q) = %q, want %q", dn, got, want)
		}
	}
}

func TestResolveRole(t *testing.T) {
	adminDN := "CN=taillight-admin,OU=NetOps,OU=Manual Groups,DC=vegvesen,DC=no"
	userDN := "CN=taillight-user,OU=NetOps,OU=Manual Groups,DC=vegvesen,DC=no"
	fullMap := map[string]string{adminDN: "admin", userDN: "user"}

	tests := []struct {
		name     string
		mapping  map[string]string
		memberOf []string
		allowed  bool
		isAdmin  bool
	}{
		{"admin wins over user", fullMap, []string{userDN, adminDN}, true, true},
		{"regular user", fullMap, []string{userDN}, true, false},
		{"case-insensitive DN match", fullMap, []string{strings.ToLower(adminDN)}, true, true},
		{"bare-CN map key matches full DN", map[string]string{"taillight-admin": "admin"}, []string{adminDN}, true, true},
		{"no mapped group denies", fullMap, []string{"CN=other,DC=x"}, false, false},
		{"empty memberOf denies", fullMap, nil, false, false},
		{"empty map denies", map[string]string{}, []string{adminDN}, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Client{cfg: Config{GroupRoleMap: tt.mapping}}
			entry := &ldaplib.Entry{}
			if len(tt.memberOf) > 0 {
				entry.Attributes = []*ldaplib.EntryAttribute{
					{Name: "memberOf", Values: tt.memberOf},
				}
			}
			allowed, isAdmin := c.resolveRole(entry)
			if allowed != tt.allowed || isAdmin != tt.isAdmin {
				t.Errorf("resolveRole() = (allowed=%v, isAdmin=%v), want (allowed=%v, isAdmin=%v)",
					allowed, isAdmin, tt.allowed, tt.isAdmin)
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
