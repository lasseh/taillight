package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lasseh/taillight/internal/model"
)

const touchBufferSize = 256

type touchOp struct {
	query string
	arg   any
}

// AuthStore provides query methods for auth-related tables.
type AuthStore struct {
	pool    *pgxpool.Pool
	touchCh chan touchOp
}

// NewAuthStore creates a new AuthStore and starts the background touch worker.
func NewAuthStore(pool *pgxpool.Pool) *AuthStore {
	s := &AuthStore{
		pool:    pool,
		touchCh: make(chan touchOp, touchBufferSize),
	}
	go s.touchWorker()
	return s
}

// Stop drains the touch channel and stops the background worker.
func (s *AuthStore) Stop() {
	close(s.touchCh)
}

// touchWorker drains the touch channel and executes last-seen/last-used updates.
func (s *AuthStore) touchWorker() {
	for op := range s.touchCh {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if _, err := s.pool.Exec(ctx, op.query, op.arg); err != nil {
			slog.Warn("touch update failed", "query", op.query, "err", err)
		}
		cancel()
	}
}

// --- Users ---

// CreateUser inserts a new user and returns the created row.
func (s *AuthStore) CreateUser(ctx context.Context, username, passwordHash string, isAdmin bool) (model.User, error) {
	var u model.User
	err := s.pool.QueryRow(ctx,
		`INSERT INTO users (username, password_hash, is_admin)
		 VALUES ($1, $2, $3)
		 RETURNING id, username, email, password_hash, is_active, is_admin, auth_source, preferences, created_at, updated_at, last_login_at`,
		username, passwordHash, isAdmin,
	).Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.IsActive, &u.IsAdmin, &u.AuthSource, &u.Preferences, &u.CreatedAt, &u.UpdatedAt, &u.LastLoginAt)
	if err != nil {
		return model.User{}, fmt.Errorf("create user: %w", err)
	}
	return u, nil
}

// GetUserByUsername returns a user by case-insensitive username lookup.
func (s *AuthStore) GetUserByUsername(ctx context.Context, username string) (model.User, error) {
	var u model.User
	err := s.pool.QueryRow(ctx,
		`SELECT id, username, email, password_hash, is_active, is_admin, auth_source, preferences, created_at, updated_at, last_login_at
		 FROM users WHERE LOWER(username) = LOWER($1)`,
		username,
	).Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.IsActive, &u.IsAdmin, &u.AuthSource, &u.Preferences, &u.CreatedAt, &u.UpdatedAt, &u.LastLoginAt)
	if err != nil {
		return model.User{}, fmt.Errorf("get user by username: %w", err)
	}
	return u, nil
}

// GetUserByID returns a user by primary key.
func (s *AuthStore) GetUserByID(ctx context.Context, id [16]byte) (model.User, error) {
	var u model.User
	err := s.pool.QueryRow(ctx,
		`SELECT id, username, email, password_hash, is_active, is_admin, auth_source, preferences, created_at, updated_at, last_login_at
		 FROM users WHERE id = $1`,
		id,
	).Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.IsActive, &u.IsAdmin, &u.AuthSource, &u.Preferences, &u.CreatedAt, &u.UpdatedAt, &u.LastLoginAt)
	if err != nil {
		return model.User{}, fmt.Errorf("get user by id: %w", err)
	}
	return u, nil
}

// UpdateLastLogin sets the last_login_at timestamp to now.
func (s *AuthStore) UpdateLastLogin(ctx context.Context, id [16]byte) error {
	_, err := s.pool.Exec(ctx, `UPDATE users SET last_login_at = now() WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("update last login: %w", err)
	}
	return nil
}

// ListUsers returns all users ordered by username.
func (s *AuthStore) ListUsers(ctx context.Context) ([]model.User, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, username, email, password_hash, is_active, is_admin, auth_source, preferences, created_at, updated_at, last_login_at
		 FROM users ORDER BY username`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	return collectUsers(rows)
}

// SetUserActive enables or disables a user account.
func (s *AuthStore) SetUserActive(ctx context.Context, id [16]byte, active bool) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE users SET is_active = $2, updated_at = now() WHERE id = $1`,
		id, active)
	if err != nil {
		return fmt.Errorf("set user active: %w", err)
	}
	return nil
}

// UpdateEmail changes a user's email address.
func (s *AuthStore) UpdateEmail(ctx context.Context, id [16]byte, email string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE users SET email = $2, updated_at = now() WHERE id = $1`,
		id, email)
	if err != nil {
		return fmt.Errorf("update email: %w", err)
	}
	return nil
}

// UpdatePreferences replaces a user's preferences JSON.
func (s *AuthStore) UpdatePreferences(ctx context.Context, id [16]byte, prefs json.RawMessage) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE users SET preferences = $2, updated_at = now() WHERE id = $1`,
		id, prefs)
	if err != nil {
		return fmt.Errorf("update preferences: %w", err)
	}
	return nil
}

// UpdatePassword changes a user's password hash.
func (s *AuthStore) UpdatePassword(ctx context.Context, id [16]byte, passwordHash string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE users SET password_hash = $2, updated_at = now() WHERE id = $1`,
		id, passwordHash)
	if err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	return nil
}

// UpsertLDAPUser creates or updates a user sourced from LDAP.
// On conflict the email, is_admin, and auth_source fields are updated.
// The is_active field is NOT touched — it remains under local admin control.
func (s *AuthStore) UpsertLDAPUser(ctx context.Context, username, email string, isAdmin bool) (model.User, error) {
	var u model.User
	var emailArg *string
	if email != "" {
		emailArg = &email
	}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO users (username, email, password_hash, is_admin, auth_source)
		 VALUES (LOWER($1), $2, '', $3, 'ldap')
		 ON CONFLICT (LOWER(username))
		 DO UPDATE SET
		     email = EXCLUDED.email,
		     is_admin = EXCLUDED.is_admin,
		     auth_source = 'ldap',
		     updated_at = now()
		 RETURNING id, username, email, password_hash, is_active, is_admin, auth_source, preferences, created_at, updated_at, last_login_at`,
		username, emailArg, isAdmin,
	).Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.IsActive, &u.IsAdmin, &u.AuthSource, &u.Preferences, &u.CreatedAt, &u.UpdatedAt, &u.LastLoginAt)
	if err != nil {
		return model.User{}, fmt.Errorf("upsert ldap user: %w", err)
	}
	return u, nil
}

// --- Sessions ---

// CreateSession inserts a new session row.
func (s *AuthStore) CreateSession(ctx context.Context, tokenHash string, userID [16]byte, expiresAt time.Time, ip, userAgent string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO sessions (token_hash, user_id, expires_at, ip_address, user_agent)
		 VALUES ($1, $2, $3, $4::inet, $5)`,
		tokenHash, userID, expiresAt, ip, userAgent)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

// SessionWithUser holds a session joined with its owning user.
type SessionWithUser struct {
	Session model.Session
	User    model.User
}

// GetSession looks up a session by token hash, joining the user.
// Returns pgx.ErrNoRows if the session is expired or the user is inactive.
func (s *AuthStore) GetSession(ctx context.Context, tokenHash string) (SessionWithUser, error) {
	var sw SessionWithUser
	err := s.pool.QueryRow(ctx,
		`SELECT s.token_hash, s.user_id, s.created_at, s.expires_at, s.last_seen_at,
		        s.ip_address::text, s.user_agent,
		        u.id, u.username, u.email, u.password_hash, u.is_active, u.is_admin, u.auth_source, u.preferences, u.created_at, u.updated_at, u.last_login_at
		 FROM sessions s
		 JOIN users u ON u.id = s.user_id
		 WHERE s.token_hash = $1
		   AND s.expires_at > now()
		   AND u.is_active = true`,
		tokenHash,
	).Scan(
		&sw.Session.TokenHash, &sw.Session.UserID, &sw.Session.CreatedAt,
		&sw.Session.ExpiresAt, &sw.Session.LastSeenAt, &sw.Session.IPAddress, &sw.Session.UserAgent,
		&sw.User.ID, &sw.User.Username, &sw.User.Email, &sw.User.PasswordHash, &sw.User.IsActive, &sw.User.IsAdmin, &sw.User.AuthSource, &sw.User.Preferences,
		&sw.User.CreatedAt, &sw.User.UpdatedAt, &sw.User.LastLoginAt,
	)
	if err != nil {
		return SessionWithUser{}, fmt.Errorf("get session: %w", err)
	}

	// Touch last_seen asynchronously via bounded worker.
	select {
	case s.touchCh <- touchOp{
		query: `UPDATE sessions SET last_seen_at = now() WHERE token_hash = $1`,
		arg:   tokenHash,
	}:
	default:
	}

	return sw, nil
}

// DeleteSession removes a session by token hash.
func (s *AuthStore) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenHash)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// DeleteUserSessions removes all sessions for a user.
func (s *AuthStore) DeleteUserSessions(ctx context.Context, userID [16]byte) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, userID)
	if err != nil {
		return fmt.Errorf("delete user sessions: %w", err)
	}
	return nil
}

// PruneUserSessions keeps the most recent `keep` sessions for a user and
// deletes the rest. This prevents unbounded session accumulation.
func (s *AuthStore) PruneUserSessions(ctx context.Context, userID [16]byte, keep int) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM sessions
		 WHERE user_id = $1
		   AND token_hash NOT IN (
		       SELECT token_hash FROM sessions
		       WHERE user_id = $1
		       ORDER BY created_at DESC
		       LIMIT $2
		   )`,
		userID, keep)
	if err != nil {
		return fmt.Errorf("prune user sessions: %w", err)
	}
	return nil
}

// CleanExpiredSessions deletes all expired sessions.
func (s *AuthStore) CleanExpiredSessions(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at <= now()`)
	if err != nil {
		return 0, fmt.Errorf("clean expired sessions: %w", err)
	}
	return tag.RowsAffected(), nil
}

// --- API Keys ---

// CreateAPIKey inserts a new API key row.
func (s *AuthStore) CreateAPIKey(ctx context.Context, userID [16]byte, name, keyHash, keyPrefix string, scopes []string, expiresAt *time.Time) (model.APIKeyRow, error) {
	var k model.APIKeyRow
	err := s.pool.QueryRow(ctx,
		`INSERT INTO api_keys (user_id, name, key_hash, key_prefix, scopes, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, user_id, name, key_hash, key_prefix, scopes, expires_at, revoked_at, last_used_at, created_at`,
		userID, name, keyHash, keyPrefix, scopes, expiresAt,
	).Scan(&k.ID, &k.UserID, &k.Name, &k.KeyHash, &k.KeyPrefix, &k.Scopes,
		&k.ExpiresAt, &k.RevokedAt, &k.LastUsedAt, &k.CreatedAt)
	if err != nil {
		return model.APIKeyRow{}, fmt.Errorf("create api key: %w", err)
	}
	return k, nil
}

// APIKeyWithUser holds an API key joined with its owning user.
type APIKeyWithUser struct {
	Key  model.APIKeyRow
	User model.User
}

// GetAPIKeyByHash looks up an active API key by its SHA-256 hash.
// Returns pgx.ErrNoRows if the key is revoked, expired, or the user is inactive.
func (s *AuthStore) GetAPIKeyByHash(ctx context.Context, keyHash string) (APIKeyWithUser, error) {
	var kw APIKeyWithUser
	err := s.pool.QueryRow(ctx,
		`SELECT k.id, k.user_id, k.name, k.key_hash, k.key_prefix, k.scopes,
		        k.expires_at, k.revoked_at, k.last_used_at, k.created_at,
		        u.id, u.username, u.email, u.password_hash, u.is_active, u.is_admin, u.auth_source, u.preferences, u.created_at, u.updated_at, u.last_login_at
		 FROM api_keys k
		 JOIN users u ON u.id = k.user_id
		 WHERE k.key_hash = $1
		   AND k.revoked_at IS NULL
		   AND (k.expires_at IS NULL OR k.expires_at > now())
		   AND u.is_active = true`,
		keyHash,
	).Scan(
		&kw.Key.ID, &kw.Key.UserID, &kw.Key.Name, &kw.Key.KeyHash, &kw.Key.KeyPrefix, &kw.Key.Scopes,
		&kw.Key.ExpiresAt, &kw.Key.RevokedAt, &kw.Key.LastUsedAt, &kw.Key.CreatedAt,
		&kw.User.ID, &kw.User.Username, &kw.User.Email, &kw.User.PasswordHash, &kw.User.IsActive, &kw.User.IsAdmin, &kw.User.AuthSource, &kw.User.Preferences,
		&kw.User.CreatedAt, &kw.User.UpdatedAt, &kw.User.LastLoginAt,
	)
	if err != nil {
		return APIKeyWithUser{}, fmt.Errorf("get api key by hash: %w", err)
	}

	// Touch last_used asynchronously via bounded worker.
	select {
	case s.touchCh <- touchOp{
		query: `UPDATE api_keys SET last_used_at = now() WHERE id = $1`,
		arg:   kw.Key.ID,
	}:
	default:
	}

	return kw, nil
}

// ListAPIKeysByUser returns all API keys for a user, including revoked ones.
func (s *AuthStore) ListAPIKeysByUser(ctx context.Context, userID [16]byte) ([]model.APIKeyRow, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, user_id, name, key_hash, key_prefix, scopes, expires_at, revoked_at, last_used_at, created_at
		 FROM api_keys WHERE user_id = $1 ORDER BY created_at DESC`,
		userID)
	if err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}
	defer rows.Close()

	var keys []model.APIKeyRow
	for rows.Next() {
		var k model.APIKeyRow
		if err := rows.Scan(&k.ID, &k.UserID, &k.Name, &k.KeyHash, &k.KeyPrefix, &k.Scopes,
			&k.ExpiresAt, &k.RevokedAt, &k.LastUsedAt, &k.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan api key: %w", err)
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// RevokeAPIKey marks an API key as revoked.
func (s *AuthStore) RevokeAPIKey(ctx context.Context, id [16]byte) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE api_keys SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`,
		id)
	if err != nil {
		return fmt.Errorf("revoke api key: %w", err)
	}
	return nil
}

// GetAPIKeyByID returns an API key by its primary key.
func (s *AuthStore) GetAPIKeyByID(ctx context.Context, id [16]byte) (model.APIKeyRow, error) {
	var k model.APIKeyRow
	err := s.pool.QueryRow(ctx,
		`SELECT id, user_id, name, key_hash, key_prefix, scopes, expires_at, revoked_at, last_used_at, created_at
		 FROM api_keys WHERE id = $1`,
		id,
	).Scan(&k.ID, &k.UserID, &k.Name, &k.KeyHash, &k.KeyPrefix, &k.Scopes, &k.ExpiresAt, &k.RevokedAt, &k.LastUsedAt, &k.CreatedAt)
	if err != nil {
		return model.APIKeyRow{}, fmt.Errorf("get api key by id: %w", err)
	}
	return k, nil
}

// GetSessionUser implements auth.SessionLookup.
func (s *AuthStore) GetSessionUser(ctx context.Context, tokenHash string) (*model.User, error) {
	sw, err := s.GetSession(ctx, tokenHash)
	if err != nil {
		return nil, err
	}
	return &sw.User, nil
}

// GetAPIKeyUser implements auth.APIKeyLookup.
func (s *AuthStore) GetAPIKeyUser(ctx context.Context, keyHash string) (*model.User, []string, error) {
	kw, err := s.GetAPIKeyByHash(ctx, keyHash)
	if err != nil {
		return nil, nil, err
	}
	return &kw.User, kw.Key.Scopes, nil
}

func collectUsers(rows pgx.Rows) ([]model.User, error) {
	var users []model.User
	for rows.Next() {
		var u model.User
		if err := rows.Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.IsActive, &u.IsAdmin, &u.AuthSource, &u.Preferences,
			&u.CreatedAt, &u.UpdatedAt, &u.LastLoginAt); err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, u)
	}
	return users, rows.Err()
}
