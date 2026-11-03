// Package ldap provides authentication against an LDAP directory (e.g. Active
// Directory or FreeIPA): a service-account bind, a user search, a password
// bind, and a group-to-role mapping read from the user's memberOf.
package ldap

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"

	ldaplib "github.com/go-ldap/ldap/v3"
)

// roleAdmin is the GroupRoleMap value that grants is_admin. Any other mapped
// value still authorizes login but as a regular (non-admin) user.
const roleAdmin = "admin"

// Sentinel errors for authentication outcomes. ErrNotAuthorized is returned
// only after the user's password bind succeeded; the other two need no
// password to produce.
var (
	ErrUserNotFound    = errors.New("ldap: user not found")
	ErrNotAuthorized   = errors.New("ldap: user in no mapped group")
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
	URL            string // Server URL (e.g. "ldaps://ipa.example.com:636").
	StartTLS       bool   // Use STARTTLS on port 389 instead of LDAPS.
	TLSSkipVerify  bool   // Skip TLS certificate verification (dev only).
	CABundle       string // PEM file of extra trusted CAs, added to the system roots. Empty = system roots only.
	BindDN         string // Service account DN for user lookups.
	BindPassword   string // Service account password.
	UserSearchBase string // Base DN for user searches (e.g. "cn=users,cn=accounts,dc=example,dc=com").
	UserFilter     string // Must contain exactly one %s for the escaped username.
	// GroupRoleMap maps a group (full DN or bare CN) to a role. The "admin" role
	// grants is_admin; any other value authorizes a regular user. A user whose
	// memberOf matches no entry is denied (ErrNotAuthorized).
	GroupRoleMap map[string]string
}

// Validate reports a configuration that cannot work, so it fails at startup
// instead of at the first login.
func (c Config) Validate() error {
	if c.URL == "" {
		return errors.New("url is required")
	}
	// Render once: a missing, doubled, or stray verb shows up as fmt's
	// "%!" error marker in the output.
	if !strings.Contains(c.UserFilter, "%s") || strings.Contains(buildUserFilter(c.UserFilter, "x"), "%!") {
		return fmt.Errorf("user_filter %q must contain exactly one %%s for the username", c.UserFilter)
	}
	return nil
}

// Client implements Authenticator using go-ldap.
// A fresh connection is dialed per Authenticate call (no pooling).
type Client struct {
	cfg    Config
	logger *slog.Logger
}

// NewClient creates a new LDAP Client.
func NewClient(cfg Config, logger *slog.Logger) *Client {
	return &Client{cfg: cfg, logger: logger}
}

// Authenticate performs a search-then-bind:
//  1. bind as the service account and search for the user (reading memberOf)
//  2. re-bind as the user to verify the password
//  3. map the user's groups to a role
//
// Returns ErrUserNotFound if the user does not exist (the caller may fall back
// to local auth), ErrInvalidPassword if the user bind fails with bad
// credentials, or ErrNotAuthorized if the user authenticated but belongs to no
// mapped group. Directory I/O stops when ctx ends, with an error that is none
// of these.
func (c *Client) Authenticate(ctx context.Context, username, password string) (*Result, error) {
	conn, err := c.dial(ctx)
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

	// Phase 2: bind as the user to verify the password.
	userDN := entry.DN
	if err := conn.Bind(userDN, password); err != nil {
		if ldaplib.IsErrorWithCode(err, ldaplib.LDAPResultInvalidCredentials) {
			return nil, ErrInvalidPassword
		}
		return nil, fmt.Errorf("ldap user bind: %w", err)
	}

	// Phase 3: authorize via group membership.
	allowed, isAdmin := c.resolveRole(entry)
	if !allowed {
		return nil, ErrNotAuthorized
	}

	email := entry.GetAttributeValue("mail")

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

// dial establishes a connection to the LDAP server. ctx's deadline bounds the
// connect and, for ldaps, the TLS handshake. When ctx ends the connection is
// closed: that is the only way to wake a Bind, Search or StartTLS waiting on
// a server that has stopped answering.
func (c *Client) dial(ctx context.Context) (*ldaplib.Conn, error) {
	tlsCfg, err := c.tlsConfig()
	if err != nil {
		return nil, err
	}
	dialer := &net.Dialer{Timeout: ldaplib.DefaultTimeout}
	dialer.Deadline, _ = ctx.Deadline()

	if c.cfg.StartTLS {
		conn, err := ldaplib.DialURL(c.cfg.URL, ldaplib.DialWithDialer(dialer))
		if err != nil {
			return nil, err
		}
		closeWhenDone(ctx, conn)
		if err := conn.StartTLS(tlsCfg); err != nil {
			conn.Close() //nolint:errcheck // best-effort cleanup on StartTLS failure
			return nil, fmt.Errorf("starttls: %w", err)
		}
		return conn, nil
	}

	conn, err := ldaplib.DialURL(c.cfg.URL, ldaplib.DialWithTLSConfig(tlsCfg), ldaplib.DialWithDialer(dialer))
	if err != nil {
		return nil, err
	}
	closeWhenDone(ctx, conn)
	return conn, nil
}

// closeWhenDone closes conn when ctx ends. Close is idempotent, so it is
// harmless after the caller has closed conn itself.
func closeWhenDone(ctx context.Context, conn *ldaplib.Conn) {
	context.AfterFunc(ctx, func() {
		conn.Close() //nolint:errcheck // the interrupted operation reports the failure
	})
}

// tlsConfig builds the TLS config. When CABundle is set its certificates are
// added to the system roots so an internal CA validates without skipping
// verification; otherwise only the system roots are used.
func (c *Client) tlsConfig() (*tls.Config, error) {
	cfg := &tls.Config{
		InsecureSkipVerify: c.cfg.TLSSkipVerify, //nolint:gosec // configurable for dev/self-signed directories
	}
	if c.cfg.CABundle == "" {
		return cfg, nil
	}

	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	pem, err := os.ReadFile(c.cfg.CABundle)
	if err != nil {
		return nil, fmt.Errorf("read ca_bundle: %w", err)
	}
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("ca_bundle %s: no certificates parsed", c.cfg.CABundle)
	}
	cfg.RootCAs = pool
	return cfg, nil
}

// buildUserFilter renders the configured user filter for a username, escaping
// the username to prevent LDAP filter injection. Extracted so the escaping is
// regression-tested independently of a live directory (audit N5).
func buildUserFilter(filterTemplate, username string) string {
	return fmt.Sprintf(filterTemplate, ldaplib.EscapeFilter(username))
}

// searchUser looks up a single user entry by username.
func (c *Client) searchUser(conn *ldaplib.Conn, username string) (*ldaplib.Entry, error) {
	filter := buildUserFilter(c.cfg.UserFilter, username)

	result, err := conn.Search(ldaplib.NewSearchRequest(
		c.cfg.UserSearchBase,
		ldaplib.ScopeWholeSubtree,
		ldaplib.NeverDerefAliases,
		0,  // size limit
		10, // time limit seconds
		false,
		filter,
		[]string{"dn", "mail", "memberOf"},
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

// resolveRole inspects the user's memberOf against GroupRoleMap. allowed is true
// if at least one group matches a mapped entry; isAdmin is true if any matched
// group maps to the admin role (admin outranks a regular user).
func (c *Client) resolveRole(entry *ldaplib.Entry) (allowed, isAdmin bool) {
	for _, group := range entry.GetAttributeValues("memberOf") {
		role, ok := roleForGroup(c.cfg.GroupRoleMap, group)
		if !ok {
			continue
		}
		allowed = true
		if strings.EqualFold(role, roleAdmin) {
			isAdmin = true
		}
	}
	return allowed, isAdmin
}

// roleForGroup matches a memberOf group DN against the configured map, accepting
// either the full DN or just the CN as the key, case-insensitively.
func roleForGroup(m map[string]string, groupDN string) (string, bool) {
	cn := cnOf(groupDN)
	for key, role := range m {
		if strings.EqualFold(key, groupDN) || strings.EqualFold(key, cn) {
			return role, true
		}
	}
	return "", false
}

// cnOf returns the CN value of a DN's first RDN
// ("CN=admins,OU=Groups,DC=x" -> "admins"). If the first RDN is not a CN, it is
// returned unchanged so a bare-CN map key still compares equal.
func cnOf(dn string) string {
	first := dn
	if i := strings.IndexByte(dn, ','); i >= 0 {
		first = dn[:i]
	}
	if len(first) >= 3 && strings.EqualFold(first[:3], "cn=") {
		return first[3:]
	}
	return first
}
