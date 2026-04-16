// Package config provides application configuration from a YAML file.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// FeaturesConfig controls which log feeds are enabled.
// When a feed is disabled, its API routes return 404 and its broker is not started.
type FeaturesConfig struct {
	Srvlog bool // Default true.
	Netlog bool // Default true.
	AppLog bool // Default true.
}

// Config holds application configuration.
type Config struct {
	DatabaseURL            string
	ListenAddr             string
	MetricsAddr            string // Separate metrics server address (empty = disabled).
	LogLevel               slog.Level
	DBMaxConns             int32
	DBMinConns             int32
	CORSAllowedOrigins     []string // Empty means allow all origins (dev mode).
	AuthEnabled            bool     // When false, all endpoints are public (no login required).
	AuthReadEndpoints      bool     // When true, read endpoints also require authentication.
	DemoMode               bool     // When true, all write endpoints return 403 Forbidden.
	CookieSecure           bool     // When true, force Secure flag on session cookies regardless of X-Forwarded-Proto.
	NotificationBufferSize int      // LISTEN/NOTIFY channel buffer size (0 = default 1024).
	NotificationWorkers    int      // Number of goroutines consuming LISTEN/NOTIFY events (0 = default 4).
	Features               FeaturesConfig
	LogShipper             LogShipperConfig
	Analysis               AnalysisConfig
	Notification           NotificationConfig
	Retention              RetentionConfig
	SMTP                   SMTPConfig
	LDAP                   LDAPConfig
}

// LDAPConfig configures LDAP (FreeIPA) authentication.
// When enabled, login attempts are first verified against the LDAP directory.
// Users authenticated via LDAP are synced to the local database for session
// and API key support. Local bcrypt auth continues to work for local users.
type LDAPConfig struct {
	Enabled        bool   // Enable LDAP authentication.
	URL            string // LDAP server URL (e.g. "ldaps://ipa.example.com:636").
	StartTLS       bool   // Use STARTTLS on port 389 instead of LDAPS.
	TLSSkipVerify  bool   // Skip TLS certificate verification (dev only).
	BindDN         string // Service account DN for user lookups.
	BindPassword   string // Service account password.
	UserSearchBase string // Base DN for user searches (e.g. "cn=users,cn=accounts,dc=example,dc=com").
	UserFilter     string // LDAP filter with %s placeholder for escaped username.
	AdminGroup     string // DN of the group that maps to is_admin=true.
}

// RetentionConfig controls how long data is kept in each hypertable.
// Values are in days. Minimum 1 day to prevent accidental data loss.
type RetentionConfig struct {
	SrvlogDays          int // Default 90.
	NetlogDays          int // Default 90.
	AppLogDays          int // Default 90.
	NotificationLogDays int // Default 30.
	RsyslogStatsDays    int // Default 30.
	MetricsDays         int // Default 30.
}

// SMTPConfig holds SMTP connection settings for the email notification backend.
type SMTPConfig struct {
	Host     string // SMTP server hostname.
	Port     int    // SMTP server port (default 587).
	Username string // SMTP username.
	Password string // SMTP password.
	From     string // Sender address (default "taillight@localhost").
	TLS      bool   // Use STARTTLS (default true).
	AuthType string // Auth mechanism: "plain", "crammd5", or "" (no auth).
}

// NotificationConfig configures the pluggable notification engine.
type NotificationConfig struct {
	Enabled             bool
	RuleRefreshInterval time.Duration
	DispatchWorkers     int
	DispatchBuffer      int
	SendTimeout         time.Duration
	DefaultSilence      time.Duration
	DefaultSilenceMax   time.Duration
	DefaultCoalesce     time.Duration
}

// LogShipperConfig configures the built-in log shipper that sends taillight's
// own application logs to the applog ingest endpoint.
type LogShipperConfig struct {
	Enabled     bool          // Enable the log shipper.
	APIKey      string        // Bearer token for the ingest endpoint.
	Service     string        // Service name attached to every log entry.
	Component   string        // Optional component name.
	Host        string        // Override hostname (empty = os.Hostname()).
	MinLevel    slog.Level    // Minimum log level to ship (default: info).
	BatchSize   int           // Entries per HTTP request (0 = default 100).
	FlushPeriod time.Duration // Flush interval (0 = default 1s).
	BufferSize  int           // Buffered channel capacity (0 = default 1024).
}

// AnalysisConfig configures the LLM-based log analysis feature.
type AnalysisConfig struct {
	Enabled     bool    // Enable analysis.
	OllamaURL   string  // Ollama API URL.
	Model       string  // Model name.
	Temperature float64 // Sampling temperature.
	NumCtx      int     // Context window size.
	ScheduleAt  string  // Cron-style schedule (e.g. "03:00").
	Feed        string  // Feed to analyze: "srvlog", "netlog", or "all" (default "netlog").
}

// Load reads configuration from config.yml with environment variable overrides.
// An optional configFile path can be provided to use a specific file instead of
// searching default locations. Returns an error if the config file exists but
// cannot be parsed.
func Load(configFile ...string) (Config, error) {
	v := viper.New()

	// Defaults.
	v.SetDefault("database_url", "postgres://taillight:changeme@localhost:15432/taillight")
	v.SetDefault("listen_addr", ":8080")
	v.SetDefault("log_level", "info")
	v.SetDefault("db_max_conns", 30)
	v.SetDefault("db_min_conns", 2)
	v.SetDefault("cors_allowed_origins", []string{})
	v.SetDefault("auth_enabled", true)
	v.SetDefault("auth_read_endpoints", true)
	v.SetDefault("demo_mode", false)
	v.SetDefault("cookie_secure", false)
	v.SetDefault("notification_buffer_size", 1024)
	v.SetDefault("notification_workers", 4)
	v.SetDefault("metrics_addr", "")
	v.SetDefault("logshipper.enabled", false)
	v.SetDefault("logshipper.service", "taillight")
	v.SetDefault("logshipper.component", "server")
	v.SetDefault("logshipper.min_level", "info")
	v.SetDefault("features.srvlog", true)
	v.SetDefault("features.netlog", true)
	v.SetDefault("features.applog", true)
	v.SetDefault("analysis.enabled", false)
	v.SetDefault("analysis.ollama_url", "http://localhost:11434")
	v.SetDefault("analysis.model", "llama3")
	v.SetDefault("analysis.temperature", 0.3)
	v.SetDefault("analysis.num_ctx", 8192)
	v.SetDefault("analysis.schedule_at", "03:00")
	v.SetDefault("analysis.feed", "netlog")
	v.SetDefault("retention.srvlog_days", 90)
	v.SetDefault("retention.netlog_days", 90)
	v.SetDefault("retention.applog_days", 90)
	v.SetDefault("retention.notification_log_days", 30)
	v.SetDefault("retention.rsyslog_stats_days", 30)
	v.SetDefault("retention.metrics_days", 30)
	v.SetDefault("smtp.host", "")
	v.SetDefault("smtp.port", 587)
	v.SetDefault("smtp.from", "taillight@localhost")
	v.SetDefault("smtp.tls", true)
	v.SetDefault("smtp.auth_type", "plain")
	v.SetDefault("ldap.enabled", false)
	v.SetDefault("ldap.url", "ldaps://ipa.example.com:636")
	v.SetDefault("ldap.starttls", false)
	v.SetDefault("ldap.tls_skip_verify", false)
	v.SetDefault("ldap.bind_dn", "")
	v.SetDefault("ldap.bind_password", "")
	v.SetDefault("ldap.user_search_base", "cn=users,cn=accounts,dc=example,dc=com")
	v.SetDefault("ldap.user_filter", "(&(objectClass=person)(uid=%s))")
	v.SetDefault("ldap.admin_group", "")
	v.SetDefault("notification.enabled", false)
	v.SetDefault("notification.rule_refresh_interval", "30s")
	v.SetDefault("notification.dispatch_workers", 4)
	v.SetDefault("notification.dispatch_buffer", 1024)
	v.SetDefault("notification.default_silence", "5m")
	v.SetDefault("notification.default_silence_max", "15m")
	v.SetDefault("notification.default_coalesce", "0s")
	v.SetDefault("notification.send_timeout", "10s")

	// Config file.
	if len(configFile) > 0 && configFile[0] != "" {
		v.SetConfigFile(configFile[0])
	} else {
		v.SetConfigName("config")
		v.SetConfigType("yml")
		v.AddConfigPath(".")
		v.AddConfigPath("/etc/taillight")
		v.AddConfigPath("/")
	}

	if err := v.ReadInConfig(); err != nil {
		if _, ok := errors.AsType[viper.ConfigFileNotFoundError](err); !ok {
			return Config{}, fmt.Errorf("read config file: %w", err)
		}
		// Config file not found is OK — use defaults and env vars.
	}

	// Environment variable overrides.
	v.AutomaticEnv()

	return Config{
		DatabaseURL:            v.GetString("database_url"),
		ListenAddr:             v.GetString("listen_addr"),
		LogLevel:               parseLogLevel(v.GetString("log_level")),
		DBMaxConns:             v.GetInt32("db_max_conns"),
		DBMinConns:             v.GetInt32("db_min_conns"),
		CORSAllowedOrigins:     v.GetStringSlice("cors_allowed_origins"),
		AuthEnabled:            v.GetBool("auth_enabled"),
		AuthReadEndpoints:      v.GetBool("auth_read_endpoints"),
		DemoMode:               v.GetBool("demo_mode"),
		CookieSecure:           v.GetBool("cookie_secure"),
		NotificationBufferSize: v.GetInt("notification_buffer_size"),
		NotificationWorkers:    v.GetInt("notification_workers"),
		MetricsAddr:            v.GetString("metrics_addr"),
		Features: FeaturesConfig{
			Srvlog: v.GetBool("features.srvlog"),
			Netlog: v.GetBool("features.netlog"),
			AppLog: v.GetBool("features.applog"),
		},
		LogShipper: LogShipperConfig{
			Enabled:     v.GetBool("logshipper.enabled"),
			APIKey:      v.GetString("logshipper.api_key"),
			Service:     v.GetString("logshipper.service"),
			Component:   v.GetString("logshipper.component"),
			Host:        v.GetString("logshipper.host"),
			MinLevel:    parseLogLevel(v.GetString("logshipper.min_level")),
			BatchSize:   v.GetInt("logshipper.batch_size"),
			FlushPeriod: v.GetDuration("logshipper.flush_period"),
			BufferSize:  v.GetInt("logshipper.buffer_size"),
		},
		Analysis: AnalysisConfig{
			Enabled:     v.GetBool("analysis.enabled"),
			OllamaURL:   v.GetString("analysis.ollama_url"),
			Model:       v.GetString("analysis.model"),
			Temperature: v.GetFloat64("analysis.temperature"),
			NumCtx:      v.GetInt("analysis.num_ctx"),
			ScheduleAt:  v.GetString("analysis.schedule_at"),
			Feed:        v.GetString("analysis.feed"),
		},
		Notification: NotificationConfig{
			Enabled:             v.GetBool("notification.enabled"),
			RuleRefreshInterval: v.GetDuration("notification.rule_refresh_interval"),
			DispatchWorkers:     v.GetInt("notification.dispatch_workers"),
			DispatchBuffer:      v.GetInt("notification.dispatch_buffer"),
			SendTimeout:         v.GetDuration("notification.send_timeout"),
			DefaultSilence:      v.GetDuration("notification.default_silence"),
			DefaultSilenceMax:   v.GetDuration("notification.default_silence_max"),
			DefaultCoalesce:     v.GetDuration("notification.default_coalesce"),
		},
		LDAP: LDAPConfig{
			Enabled:        v.GetBool("ldap.enabled"),
			URL:            v.GetString("ldap.url"),
			StartTLS:       v.GetBool("ldap.starttls"),
			TLSSkipVerify:  v.GetBool("ldap.tls_skip_verify"),
			BindDN:         v.GetString("ldap.bind_dn"),
			BindPassword:   v.GetString("ldap.bind_password"),
			UserSearchBase: v.GetString("ldap.user_search_base"),
			UserFilter:     v.GetString("ldap.user_filter"),
			AdminGroup:     v.GetString("ldap.admin_group"),
		},
		SMTP: SMTPConfig{
			Host:     v.GetString("smtp.host"),
			Port:     v.GetInt("smtp.port"),
			Username: v.GetString("smtp.username"),
			Password: v.GetString("smtp.password"),
			From:     v.GetString("smtp.from"),
			TLS:      v.GetBool("smtp.tls"),
			AuthType: v.GetString("smtp.auth_type"),
		},
		Retention: RetentionConfig{
			SrvlogDays:          max(v.GetInt("retention.srvlog_days"), 1),
			NetlogDays:          max(v.GetInt("retention.netlog_days"), 1),
			AppLogDays:          max(v.GetInt("retention.applog_days"), 1),
			NotificationLogDays: max(v.GetInt("retention.notification_log_days"), 1),
			RsyslogStatsDays:    max(v.GetInt("retention.rsyslog_stats_days"), 1),
			MetricsDays:         max(v.GetInt("retention.metrics_days"), 1),
		},
	}, nil
}

func parseLogLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
