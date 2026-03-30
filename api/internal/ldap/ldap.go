// Package ldap provides LDAP authentication against FreeIPA directories.
package ldap

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	ldaplib "github.com/go-ldap/ldap/v3"
)

// Sentinel errors for authentication outcomes.
var (
	ErrUserNotFound    = errors.New("ldap: user not found")
	ErrAccountLocked   = errors.New("ldap: account locked")
	ErrInvalidPassword = errors.New("ldap: invalid password")
)

// Result holds the attributes returned from a successful LDAP authentication.
type Result struct {
	Username string
	Email    string
	IsAdmin  bool
	DN       string
}

// Authenticator defines the interface for LDAP authentication.
// Implementations are safe for concurrent use.
type Authenticator interface {
	Authenticate(ctx context.Context, username, password string) (*Result, error)
}

// Config holds LDAP connection and search settings.
type Config struct {
	URL            string
	StartTLS       bool
	TLSSkipVerify  bool
	BindDN         string
	BindPassword   string
	UserSearchBase string
	UserFilter     string // Must contain exactly one %s for the escaped username.
	AdminGroup     string // Full DN of the admin group.
}

// Client implements Authenticator using go-ldap against a FreeIPA directory.
// A fresh connection is dialed per Authenticate call (no pooling).
type Client struct {
	cfg    Config
	logger *slog.Logger
}

// NewClient creates a new LDAP Client.
func NewClient(cfg Config, logger *slog.Logger) *Client {
	return &Client{cfg: cfg, logger: logger}
}

// Authenticate performs a two-phase LDAP bind:
//  1. Service account bind → search for user → check account status
//  2. User bind to verify password
//
// Returns ErrUserNotFound if the user does not exist in the directory,
// ErrAccountLocked if nsAccountLock is TRUE, or ErrInvalidPassword if
// the user bind fails with invalid credentials.
func (c *Client) Authenticate(_ context.Context, username, password string) (*Result, error) {
	conn, err := c.dial()
	if err != nil {
		return nil, fmt.Errorf("ldap connect: %w", err)
	}
	defer conn.Close() //nolint:errcheck // best-effort cleanup

	// Phase 1: bind as service account and search for the user.
	if err := conn.Bind(c.cfg.BindDN, c.cfg.BindPassword); err != nil {
		return nil, fmt.Errorf("ldap service bind: %w", err)
	}

	entry, err := c.searchUser(conn, username)
	if err != nil {
		return nil, err
	}

	// Check account lock before attempting user bind.
	if isAccountLocked(entry) {
		return nil, ErrAccountLocked
	}

	// Phase 2: bind as the user to verify the password.
	userDN := entry.DN
	if err := conn.Bind(userDN, password); err != nil {
		if ldaplib.IsErrorWithCode(err, ldaplib.LDAPResultInvalidCredentials) {
			return nil, ErrInvalidPassword
		}
		return nil, fmt.Errorf("ldap user bind: %w", err)
	}

	email := entry.GetAttributeValue("mail")
	isAdmin := c.checkAdminGroup(entry)

	c.logger.Info("ldap authentication succeeded",
		"username", username,
		"dn", userDN,
		"is_admin", isAdmin,
	)

	return &Result{
		Username: username,
		Email:    email,
		IsAdmin:  isAdmin,
		DN:       userDN,
	}, nil
}

// dial establishes a connection to the LDAP server.
func (c *Client) dial() (*ldaplib.Conn, error) {
	tlsCfg := &tls.Config{
		InsecureSkipVerify: c.cfg.TLSSkipVerify, //nolint:gosec // configurable for dev
	}

	if c.cfg.StartTLS {
		conn, err := ldaplib.DialURL(c.cfg.URL)
		if err != nil {
			return nil, err
		}
		if err := conn.StartTLS(tlsCfg); err != nil {
			conn.Close() //nolint:errcheck // best-effort cleanup on StartTLS failure
			return nil, fmt.Errorf("starttls: %w", err)
		}
		return conn, nil
	}

	return ldaplib.DialURL(c.cfg.URL, ldaplib.DialWithTLSConfig(tlsCfg))
}

// searchUser looks up a single user entry by username.
func (c *Client) searchUser(conn *ldaplib.Conn, username string) (*ldaplib.Entry, error) {
	filter := fmt.Sprintf(c.cfg.UserFilter, ldaplib.EscapeFilter(username))

	result, err := conn.Search(ldaplib.NewSearchRequest(
		c.cfg.UserSearchBase,
		ldaplib.ScopeWholeSubtree,
		ldaplib.NeverDerefAliases,
		0,  // size limit
		10, // time limit seconds
		false,
		filter,
		[]string{"dn", "mail", "memberOf", "nsAccountLock"},
		nil,
	))
	if err != nil {
		return nil, fmt.Errorf("ldap search: %w", err)
	}

	if len(result.Entries) == 0 {
		return nil, ErrUserNotFound
	}
	if len(result.Entries) > 1 {
		return nil, fmt.Errorf("ldap search: ambiguous result (%d entries for %q)", len(result.Entries), username)
	}

	return result.Entries[0], nil
}

// isAccountLocked checks the nsAccountLock attribute used by FreeIPA/389DS.
// The attribute is "TRUE" when the account is disabled and absent when active.
func isAccountLocked(entry *ldaplib.Entry) bool {
	return strings.EqualFold(entry.GetAttributeValue("nsAccountLock"), "TRUE")
}

// checkAdminGroup checks whether the user is a member of the configured admin group.
func (c *Client) checkAdminGroup(entry *ldaplib.Entry) bool {
	if c.cfg.AdminGroup == "" {
		return false
	}
	for _, group := range entry.GetAttributeValues("memberOf") {
		if strings.EqualFold(group, c.cfg.AdminGroup) {
			return true
		}
	}
	return false
}
