package model

import (
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// User represents a row from the users table.
type User struct {
	ID           pgtype.UUID        `json:"id"`
	Username     string             `json:"username"`
	Email        *string            `json:"email,omitempty"`
	PasswordHash string             `json:"-"`
	IsActive     bool               `json:"is_active"`
	IsAdmin      bool               `json:"is_admin"`
	AuthSource   string             `json:"auth_source"`
	Preferences  json.RawMessage    `json:"preferences"`
	CreatedAt    time.Time          `json:"created_at"`
	UpdatedAt    time.Time          `json:"updated_at"`
	LastLoginAt  pgtype.Timestamptz `json:"last_login_at"`
}

// Session represents a row from the sessions table.
type Session struct {
	TokenHash  string      `json:"-"`
	UserID     pgtype.UUID `json:"user_id"`
	CreatedAt  time.Time   `json:"created_at"`
	ExpiresAt  time.Time   `json:"expires_at"`
	LastSeenAt time.Time   `json:"last_seen_at"`
	IPAddress  *string     `json:"ip_address,omitempty"`
	UserAgent  string      `json:"user_agent"`
}

// APIKeyRow represents a row from the api_keys table.
type APIKeyRow struct {
	ID         pgtype.UUID        `json:"id"`
	UserID     pgtype.UUID        `json:"user_id"`
	Name       string             `json:"name"`
	KeyHash    string             `json:"-"`
	KeyPrefix  string             `json:"key_prefix"`
	Scopes     []string           `json:"scopes"`
	ExpiresAt  pgtype.Timestamptz `json:"expires_at"`
	RevokedAt  pgtype.Timestamptz `json:"revoked_at"`
	LastUsedAt pgtype.Timestamptz `json:"last_used_at"`
	CreatedAt  time.Time          `json:"created_at"`
}
