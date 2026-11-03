package ldap

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	ldaplib "github.com/go-ldap/ldap/v3"
)

// stalledDirectory listens on loopback and passes each accepted connection to
// serve, which never answers the request the client waits on. It returns the
// listener address.
func stalledDirectory(t *testing.T, serve func(net.Conn)) string {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() }) //nolint:errcheck // test teardown
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close() //nolint:errcheck // test teardown
				serve(conn)
			}()
		}
	}()
	return ln.Addr().String()
}

// silent reads until the client hangs up and never writes.
func silent(conn net.Conn) {
	io.Copy(io.Discard, conn) //nolint:errcheck // the client hanging up ends the read
}

// startTLSOK is a StartTLS success response (an ExtendedResponse with
// resultCode 0) to message ID 1, the first request on a new connection.
var startTLSOK = []byte{0x30, 0x0c, 0x02, 0x01, 0x01, 0x78, 0x07, 0x0a, 0x01, 0x00, 0x04, 0x00, 0x04, 0x00}

// acceptStartTLSThenStall accepts the StartTLS request and then never answers
// the TLS handshake.
func acceptStartTLSThenStall(conn net.Conn) {
	if _, err := conn.Read(make([]byte, 512)); err != nil {
		return
	}
	if _, err := conn.Write(startTLSOK); err != nil {
		return
	}
	silent(conn)
}

// TestAuthenticateReturnsWhenContextEnds checks that a directory which accepts
// the connection and then goes silent holds Authenticate only until ctx ends,
// at every step that waits on the server, and that the result is an outage
// error, which login answers by falling back to the local table.
func TestAuthenticateReturnsWhenContextEnds(t *testing.T) {
	tests := []struct {
		name     string
		scheme   string
		startTLS bool
		serve    func(net.Conn)
	}{
		{name: "service bind", scheme: "ldap", serve: silent},
		{name: "starttls response", scheme: "ldap", startTLS: true, serve: silent},
		{name: "starttls handshake", scheme: "ldap", startTLS: true, serve: acceptStartTLSThenStall},
		{name: "ldaps handshake", scheme: "ldaps", serve: silent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr := stalledDirectory(t, tt.serve)
			c := NewClient(Config{
				URL:      tt.scheme + "://" + addr,
				StartTLS: tt.startTLS,
				// The StartTLS client needs a ServerName or this to begin
				// the handshake; the server never answers it either way.
				TLSSkipVerify:  true,
				BindDN:         "cn=svc,dc=example,dc=com",
				BindPassword:   "svc-password",
				UserSearchBase: "dc=example,dc=com",
				UserFilter:     "(uid=%s)",
			}, slog.New(slog.DiscardHandler))

			const budget = 200 * time.Millisecond
			ctx, cancel := context.WithTimeout(context.Background(), budget)
			defer cancel()

			start := time.Now()
			done := make(chan error, 1)
			go func() {
				_, err := c.Authenticate(ctx, "alice", "pw")
				done <- err
			}()
			select {
			case err := <-done:
				if err == nil || errors.Is(err, ErrUserNotFound) || errors.Is(err, ErrInvalidPassword) || errors.Is(err, ErrNotAuthorized) {
					t.Fatalf("err = %v, want an outage error", err)
				}
				// Returning well before the deadline means the client failed
				// on its own and never reached the step under test.
				if elapsed := time.Since(start); elapsed < budget/2 || elapsed > budget+2*time.Second {
					t.Errorf("Authenticate returned after %v (%v), want about %v", elapsed, err, budget)
				}
			case <-time.After(budget + 5*time.Second):
				t.Fatal("Authenticate still waiting on a silent directory after its context ended")
			}
		})
	}
}

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
