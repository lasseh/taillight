package auth

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"slices"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/jackc/pgx/v5"

	"github.com/lasseh/taillight/internal/ldap"
	"github.com/lasseh/taillight/internal/model"
	oidcauth "github.com/lasseh/taillight/internal/oidc"
)

type fakeDirectory struct {
	result ldap.Result
	err    error
}

func (d fakeDirectory) Authenticate(context.Context, string, string) (*ldap.Result, error) {
	if d.err != nil {
		return nil, d.err
	}
	return &d.result, nil
}

// fakeLoginStore holds one local row keyed by username and records upserts
// and revocations.
type fakeLoginStore struct {
	users      map[string]model.User
	getErr     error
	ldapUpsert *model.User // set when UpsertLDAPUser runs
	oidcUser   model.User
	revoked    []string // usernames passed to RevokeLDAPUser
	revokeErr  error
}

func (s *fakeLoginStore) RevokeLDAPUser(_ context.Context, username string) error {
	s.revoked = append(s.revoked, username)
	return s.revokeErr
}

func (s *fakeLoginStore) GetUserByUsername(ctx context.Context, username string) (model.User, error) {
	if s.getErr != nil {
		return model.User{}, s.getErr
	}
	// A real store fails a lookup on an ended context too.
	if err := ctx.Err(); err != nil {
		return model.User{}, err
	}
	u, ok := s.users[username]
	if !ok {
		return model.User{}, pgx.ErrNoRows
	}
	return u, nil
}

func (s *fakeLoginStore) UpsertLDAPUser(_ context.Context, username, email string, isAdmin bool) (model.User, error) {
	u := model.User{Username: username, AuthSource: SourceLDAP, IsAdmin: isAdmin, IsActive: true}
	if existing, ok := s.users[username]; ok {
		u.IsActive = existing.IsActive // is_active stays under local admin control.
	}
	s.ldapUpsert = &u
	return u, nil
}

func (s *fakeLoginStore) UpsertOIDCUser(context.Context, string, string, string, string, bool) (model.User, error) {
	return s.oidcUser, nil
}

// countComparisons counts bcrypt comparisons until the test ends.
func countComparisons(t *testing.T) *int {
	t.Helper()
	n := 0
	orig := compareHashAndPassword
	compareHashAndPassword = func(hash, password []byte) error {
		n++
		return orig(hash, password)
	}
	t.Cleanup(func() { compareHashAndPassword = orig })
	return &n
}

func mustHash(t *testing.T, pw string) string {
	t.Helper()
	h, err := HashPassword(pw)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestLoginPassword(t *testing.T) {
	hash := mustHash(t, "hunter2")
	users := func() map[string]model.User {
		return map[string]model.User{
			"alice": {Username: "alice", PasswordHash: hash, AuthSource: SourceLocal, IsActive: true},
			"bob":   {Username: "bob", AuthSource: SourceLDAP, IsActive: true},
			"carol": {Username: "carol", PasswordHash: hash, AuthSource: SourceLocal, IsActive: false},
			"sso":   {Username: "sso", AuthSource: SourceOIDC, IsActive: true},
		}
	}
	dirOK := fakeDirectory{result: ldap.Result{Username: "alice", IsAdmin: true}}

	tests := []struct {
		name        string
		dir         ldap.Authenticator
		getErr      error
		revokeErr   error
		username    string
		password    string
		wantUser    string
		wantSource  string
		wantDenied  string // substring of the denial reason; "" = success
		wantInfra   bool
		wantRevoked bool // RevokeLDAPUser ran for username
		// wantCompares is the number of bcrypt comparisons. Every password
		// denial runs exactly one, so response time does not tell the
		// denials apart.
		wantCompares int
	}{
		{name: "local password", username: "alice", password: "hunter2", wantUser: "alice", wantSource: SourceLocal, wantCompares: 1},
		{name: "local wrong password", username: "alice", password: "nope", wantDenied: "wrong password", wantCompares: 1},
		{name: "unknown user", username: "mallory", password: "x", wantDenied: "unknown user", wantCompares: 1},
		{name: "ldap row refused locally", username: "bob", password: "x", wantDenied: "external-auth user attempted local auth", wantSource: SourceLDAP, wantCompares: 1},
		{name: "oidc row refused locally", username: "sso", password: "x", wantDenied: "external-auth user attempted local auth", wantSource: SourceOIDC, wantCompares: 1},
		{name: "inactive local account", username: "carol", password: "hunter2", wantDenied: "inactive account", wantCompares: 1},
		{name: "store failure", getErr: errors.New("db down"), username: "alice", password: "hunter2", wantInfra: true},

		// LDAP success takes over the local row of the same name.
		{name: "ldap success", dir: dirOK, username: "alice", password: "dirpw", wantUser: "alice", wantSource: SourceLDAP},
		{name: "ldap user not found falls through", dir: fakeDirectory{err: ldap.ErrUserNotFound}, username: "alice", password: "hunter2", wantUser: "alice", wantSource: SourceLocal, wantCompares: 1},
		{name: "ldap outage falls through", dir: fakeDirectory{err: errors.New("connection refused")}, username: "alice", password: "hunter2", wantUser: "alice", wantSource: SourceLocal, wantCompares: 1},
		{name: "ldap wrong password does not fall through", dir: fakeDirectory{err: ldap.ErrInvalidPassword}, username: "alice", password: "hunter2", wantDenied: "LDAP wrong password", wantCompares: 1},
		{name: "ldap no group does not fall through", dir: fakeDirectory{err: ldap.ErrNotAuthorized}, username: "alice", password: "hunter2", wantDenied: "LDAP user not in any authorized group", wantRevoked: true, wantCompares: 1},
		{name: "ldap no group with revoke failure", dir: fakeDirectory{err: ldap.ErrNotAuthorized}, revokeErr: errors.New("db down"), username: "bob", password: "dirpw", wantInfra: true, wantRevoked: true, wantCompares: 1},
		{name: "ldap success on inactive row", dir: fakeDirectory{result: ldap.Result{Username: "carol"}}, username: "carol", password: "dirpw", wantDenied: "inactive account"},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compares := countComparisons(t)
			store := &fakeLoginStore{users: users(), getErr: tt.getErr, revokeErr: tt.revokeErr}
			user, err := NewLogin(store, tt.dir).Password(context.Background(), logger, tt.username, tt.password)

			if *compares != tt.wantCompares {
				t.Errorf("bcrypt comparisons = %d, want %d", *compares, tt.wantCompares)
			}

			// Only a directory refusal after the password bind may revoke;
			// every other outcome, including the denials that need no
			// password, must leave the user's row and sessions alone.
			var wantRevoked []string
			if tt.wantRevoked {
				wantRevoked = []string{tt.username}
			}
			if !slices.Equal(store.revoked, wantRevoked) {
				t.Errorf("RevokeLDAPUser calls = %v, want %v", store.revoked, wantRevoked)
			}

			switch {
			case tt.wantInfra:
				if err == nil || errors.Is(err, ErrInvalidCredentials) {
					t.Fatalf("err = %v, want an infrastructure error", err)
				}
			case tt.wantDenied != "":
				d, ok := errors.AsType[*DeniedError](err)
				if !ok || !errors.Is(err, ErrInvalidCredentials) {
					t.Fatalf("err = %v, want a *DeniedError matching ErrInvalidCredentials", err)
				}
				// The reason becomes the "login failed: <reason>" log line.
				if d.Reason != tt.wantDenied || d.AuthSource != tt.wantSource {
					t.Errorf("denial = %q/%q, want %q/%q", d.Reason, d.AuthSource, tt.wantDenied, tt.wantSource)
				}
			default:
				if err != nil {
					t.Fatalf("err = %v", err)
				}
				if user.Username != tt.wantUser || user.AuthSource != tt.wantSource {
					t.Errorf("user = %s/%s, want %s/%s", user.Username, user.AuthSource, tt.wantUser, tt.wantSource)
				}
			}
		})
	}
}

// TestLoginFallsBackWhenDirectoryStalls runs a real directory client against a
// loopback listener that accepts and never answers. Login must give up on the
// directory within directoryTimeout and log a local row in on the request
// context, which still has most of its time left.
func TestLoginFallsBackWhenDirectoryStalls(t *testing.T) {
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
				defer conn.Close()        //nolint:errcheck // test teardown
				io.Copy(io.Discard, conn) //nolint:errcheck // read until the client hangs up
			}()
		}
	}()

	orig := directoryTimeout
	directoryTimeout = 200 * time.Millisecond
	t.Cleanup(func() { directoryTimeout = orig })

	dir := ldap.NewClient(ldap.Config{
		URL:            "ldap://" + ln.Addr().String(),
		BindDN:         "cn=svc,dc=example,dc=com",
		BindPassword:   "svc-password", // a bind with an empty password never reaches the server
		UserSearchBase: "dc=example,dc=com",
		UserFilter:     "(uid=%s)",
	}, slog.New(slog.DiscardHandler))
	hash, err := bcrypt.GenerateFromPassword([]byte("hunter2"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	store := &fakeLoginStore{users: map[string]model.User{
		"alice": {Username: "alice", PasswordHash: string(hash), AuthSource: SourceLocal, IsActive: true},
	}}
	compares := countComparisons(t)

	// The route's own timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	type result struct {
		user model.User
		err  error
	}
	done := make(chan result, 1)
	start := time.Now()
	go func() {
		user, err := NewLogin(store, dir).Password(ctx, slog.New(slog.DiscardHandler), "alice", "hunter2")
		done <- result{user, err}
	}()
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("err = %v, want the local row to log in", r.err)
		}
		if r.user.Username != "alice" || r.user.AuthSource != SourceLocal {
			t.Errorf("user = %s/%s, want alice/%s", r.user.Username, r.user.AuthSource, SourceLocal)
		}
		if elapsed := time.Since(start); elapsed > directoryTimeout+2*time.Second {
			t.Errorf("login took %v, want about %v", elapsed, directoryTimeout)
		}
		if *compares != 1 {
			t.Errorf("bcrypt comparisons = %d, want 1", *compares)
		}
	case <-time.After(directoryTimeout + 5*time.Second):
		t.Fatal("login still waiting on a silent directory")
	}
}

func TestLoginOIDC(t *testing.T) {
	ident := oidcauth.Identity{Issuer: "https://idp", Subject: "123", Username: "dana"}

	store := &fakeLoginStore{oidcUser: model.User{Username: "dana", AuthSource: SourceOIDC, IsActive: true}}
	if u, err := NewLogin(store, nil).OIDC(context.Background(), ident); err != nil || u.Username != "dana" {
		t.Fatalf("OIDC = %v, %v", u, err)
	}

	store.oidcUser.IsActive = false
	if _, err := NewLogin(store, nil).OIDC(context.Background(), ident); !errors.Is(err, ErrInactive) {
		t.Fatalf("inactive OIDC user: err = %v, want ErrInactive", err)
	}
}
