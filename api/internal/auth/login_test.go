package auth

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

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

// fakeLoginStore holds one local row keyed by username and records upserts.
type fakeLoginStore struct {
	users      map[string]model.User
	getErr     error
	ldapUpsert *model.User // set when UpsertLDAPUser runs
	oidcUser   model.User
}

func (s *fakeLoginStore) GetUserByUsername(_ context.Context, username string) (model.User, error) {
	if s.getErr != nil {
		return model.User{}, s.getErr
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
		name       string
		dir        ldap.Authenticator
		getErr     error
		username   string
		password   string
		wantUser   string
		wantSource string
		wantDenied string // substring of the denial reason; "" = success
		wantInfra  bool
	}{
		{name: "local password", username: "alice", password: "hunter2", wantUser: "alice", wantSource: SourceLocal},
		{name: "local wrong password", username: "alice", password: "nope", wantDenied: "wrong password"},
		{name: "unknown user", username: "mallory", password: "x", wantDenied: "unknown user"},
		{name: "ldap row refused locally", username: "bob", password: "x", wantDenied: "external-auth user attempted local auth", wantSource: SourceLDAP},
		{name: "oidc row refused locally", username: "sso", password: "x", wantDenied: "external-auth user attempted local auth", wantSource: SourceOIDC},
		{name: "inactive local account", username: "carol", password: "hunter2", wantDenied: "inactive account"},
		{name: "store failure", getErr: errors.New("db down"), username: "alice", password: "hunter2", wantInfra: true},

		// LDAP success takes over the local row of the same name.
		{name: "ldap success", dir: dirOK, username: "alice", password: "dirpw", wantUser: "alice", wantSource: SourceLDAP},
		{name: "ldap user not found falls through", dir: fakeDirectory{err: ldap.ErrUserNotFound}, username: "alice", password: "hunter2", wantUser: "alice", wantSource: SourceLocal},
		{name: "ldap outage falls through", dir: fakeDirectory{err: errors.New("connection refused")}, username: "alice", password: "hunter2", wantUser: "alice", wantSource: SourceLocal},
		{name: "ldap wrong password does not fall through", dir: fakeDirectory{err: ldap.ErrInvalidPassword}, username: "alice", password: "hunter2", wantDenied: "LDAP wrong password"},
		{name: "ldap no group does not fall through", dir: fakeDirectory{err: ldap.ErrNotAuthorized}, username: "alice", password: "hunter2", wantDenied: "LDAP user not in any authorized group"},
		{name: "ldap success on inactive row", dir: fakeDirectory{result: ldap.Result{Username: "carol"}}, username: "carol", password: "dirpw", wantDenied: "inactive account"},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeLoginStore{users: users(), getErr: tt.getErr}
			user, err := NewLogin(store, tt.dir).Password(context.Background(), logger, tt.username, tt.password)

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
