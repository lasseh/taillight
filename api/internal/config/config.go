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
	NotificationBufferSize int      // LISTEN/NOTIFY channel buffer size (0 = default 1024).
	LogShipper             LogShipperConfig
	Analysis               AnalysisConfig
	Notification           NotificationConfig
}

// NotificationConfig configures the pluggable notification engine.
type NotificationConfig struct {
	Enabled             bool
	RuleRefreshInterval time.Duration
	DispatchWorkers     int
	DispatchBuffer      int
	DefaultBurstWindow  time.Duration
	DefaultCooldown     time.Duration
	SendTimeout         time.Duration
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
}

// Load reads configuration from config.yaml with environment variable overrides.
// Returns an error if the config file exists but cannot be parsed.
func Load() (Config, error) {
	v := viper.New()

	// Defaults.
	v.SetDefault("database_url", "postgres://taillight:changeme@localhost:15432/taillight")
	v.SetDefault("listen_addr", ":8080")
	v.SetDefault("log_level", "info")
	v.SetDefault("db_max_conns", 10)
	v.SetDefault("db_min_conns", 2)
	v.SetDefault("cors_allowed_origins", []string{})
	v.SetDefault("auth_enabled", true)
	v.SetDefault("auth_read_endpoints", true)
	v.SetDefault("notification_buffer_size", 1024)
	v.SetDefault("metrics_addr", "")
	v.SetDefault("logshipper.enabled", false)
	v.SetDefault("logshipper.service", "taillight")
	v.SetDefault("logshipper.component", "server")
	v.SetDefault("logshipper.min_level", "info")
	v.SetDefault("analysis.enabled", false)
	v.SetDefault("analysis.ollama_url", "http://localhost:11434")
	v.SetDefault("analysis.model", "llama3")
	v.SetDefault("analysis.temperature", 0.3)
	v.SetDefault("analysis.num_ctx", 8192)
	v.SetDefault("analysis.schedule_at", "03:00")
	v.SetDefault("notification.enabled", false)
	v.SetDefault("notification.rule_refresh_interval", "30s")
	v.SetDefault("notification.dispatch_workers", 4)
	v.SetDefault("notification.dispatch_buffer", 1024)
	v.SetDefault("notification.default_burst_window", "30s")
	v.SetDefault("notification.default_cooldown", "5m")
	v.SetDefault("notification.send_timeout", "10s")

	// Config file.
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")
	v.AddConfigPath("/etc/taillight")
	v.AddConfigPath("/")

	if err := v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if !errors.As(err, &notFound) {
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
		NotificationBufferSize: v.GetInt("notification_buffer_size"),
		MetricsAddr:            v.GetString("metrics_addr"),
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
		},
		Notification: NotificationConfig{
			Enabled:             v.GetBool("notification.enabled"),
			RuleRefreshInterval: v.GetDuration("notification.rule_refresh_interval"),
			DispatchWorkers:     v.GetInt("notification.dispatch_workers"),
			DispatchBuffer:      v.GetInt("notification.dispatch_buffer"),
			DefaultBurstWindow:  v.GetDuration("notification.default_burst_window"),
			DefaultCooldown:     v.GetDuration("notification.default_cooldown"),
			SendTimeout:         v.GetDuration("notification.send_timeout"),
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
