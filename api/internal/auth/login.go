package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"

	"github.com/lasseh/taillight/internal/ldap"
	"github.com/lasseh/taillight/internal/model"
	oidcauth "github.com/lasseh/taillight/internal/oidc"
)

// Values of users.auth_source.
const (
	SourceLocal = "local"
	SourceLDAP  = "ldap"
	SourceOIDC  = "oidc"
)

// IsExternalSource reports whether an auth source is managed outside
// taillight (LDAP directory or OIDC provider) and therefore carries no local
// password.
func IsExternalSource(source string) bool {
	return source == SourceLDAP || source == SourceOIDC
}

// Login errors. Every password denial is a *DeniedError, which matches
// ErrInvalidCredentials, so a caller can log the reason and send the client
// one indistinguishable message. ErrInactive marks a disabled OIDC account.
var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrInactive           = errors.New("inactive account")
)

// DeniedError is a refused password login. Reason is for the log only.
type DeniedError struct {
	Reason     string
	AuthSource string // set when a local login hit an externally managed user
}

func (e *DeniedError) Error() string { return "login denied: " + e.Reason }

// Unwrap makes errors.Is(err, ErrInvalidCredentials) hold.
func (e *DeniedError) Unwrap() error { return ErrInvalidCredentials }

func denied(reason string) error { return &DeniedError{Reason: reason} }

// LoginStore is the user lookup and provisioning surface login resolution
// needs.
type LoginStore interface {
	GetUserByUsername(ctx context.Context, username string) (model.User, error)
	UpsertLDAPUser(ctx context.Context, username, email string, isAdmin bool) (model.User, error)
	UpsertOIDCUser(ctx context.Context, issuer, subject, username, email string, isAdmin bool) (model.User, error)
}

// Login turns credentials into a user. It owns the account linking policy:
//
//   - LDAP users are matched by username. A successful directory login takes
//     over any existing row with that username, local or OIDC, and syncs its
//     email and admin flag from the directory.
//   - OIDC users are matched by (issuer, subject) only and are never linked
//     to an existing account; a taken username gets a numeric suffix.
//   - Local password login is refused for LDAP and OIDC rows.
//   - An inactive account is refused whatever its source.
type Login struct {
	store LoginStore
	ldap  ldap.Authenticator // nil when LDAP is disabled.
}

// NewLogin returns a Login. dir may be nil when LDAP is disabled.
func NewLogin(store LoginStore, dir ldap.Authenticator) *Login {
	return &Login{store: store, ldap: dir}
}

// Password resolves a username and password: the LDAP directory first when
// configured, then the local users table. A directory that does not know the
// user, or that is unreachable, falls through to local login; a directory
// that rejects the password or the user's groups does not. It returns a
// *DeniedError for a denial and any other error for an infrastructure
// failure. logger receives the directory
// outage that triggers the fallback.
func (l *Login) Password(ctx context.Context, logger *slog.Logger, username, password string) (model.User, error) {
	user, ok, err := l.directory(ctx, logger, username, password)
	if err != nil {
		return model.User{}, err
	}
	if !ok {
		user, err = l.local(ctx, username, password)
		if err != nil {
			return model.User{}, err
		}
	}
	if !user.IsActive {
		return model.User{}, denied("inactive account")
	}
	return user, nil
}

// directory tries LDAP. ok is false when login should fall through to the
// local table.
func (l *Login) directory(ctx context.Context, logger *slog.Logger, username, password string) (user model.User, ok bool, err error) {
	if l.ldap == nil {
		return model.User{}, false, nil
	}
	result, err := l.ldap.Authenticate(ctx, username, password)
	switch {
	case err == nil:
		user, err := l.store.UpsertLDAPUser(ctx, result.Username, result.Email, result.IsAdmin)
		if err != nil {
			return model.User{}, false, fmt.Errorf("upsert ldap user: %w", err)
		}
		return user, true, nil
	case errors.Is(err, ldap.ErrUserNotFound):
		return model.User{}, false, nil
	case errors.Is(err, ldap.ErrNotAuthorized):
		return model.User{}, false, denied("LDAP user not in any authorized group")
	case errors.Is(err, ldap.ErrInvalidPassword):
		return model.User{}, false, denied("LDAP wrong password")
	default:
		logger.Error("login: LDAP error, falling back to local auth", "err", err, "username", username)
		return model.User{}, false, nil
	}
}

// local checks the password against the users table. It runs bcrypt on every
// path, including unknown and externally managed users, so response time does
// not reveal which usernames exist.
func (l *Login) local(ctx context.Context, username, password string) (model.User, error) {
	user, err := l.store.GetUserByUsername(ctx, username)
	if errors.Is(err, pgx.ErrNoRows) {
		DummyCheckPassword(password)
		return model.User{}, denied("unknown user")
	}
	if err != nil {
		return model.User{}, fmt.Errorf("get user failed: %w", err)
	}
	if IsExternalSource(user.AuthSource) {
		DummyCheckPassword(password)
		return model.User{}, &DeniedError{Reason: "external-auth user attempted local auth", AuthSource: user.AuthSource}
	}
	if err := CheckPassword(password, user.PasswordHash); err != nil {
		return model.User{}, denied("wrong password")
	}
	return user, nil
}

// OIDC returns the user for an identity the provider has verified,
// provisioning it on first login. It returns an error wrapping ErrInactive
// for a disabled account and any other error for a store failure.
func (l *Login) OIDC(ctx context.Context, ident oidcauth.Identity) (model.User, error) {
	user, err := l.store.UpsertOIDCUser(ctx, ident.Issuer, ident.Subject, ident.Username, ident.Email, ident.IsAdmin)
	if err != nil {
		return model.User{}, fmt.Errorf("upsert oidc user: %w", err)
	}
	if !user.IsActive {
		return model.User{}, ErrInactive
	}
	return user, nil
}
