package config

import (
	"log/slog"
	"testing"
	"time"
)

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		input string
		want  slog.Level
	}{
		{input: "debug", want: slog.LevelDebug},
		{input: "DEBUG", want: slog.LevelDebug},
		{input: "info", want: slog.LevelInfo},
		{input: "INFO", want: slog.LevelInfo},
		{input: "warn", want: slog.LevelWarn},
		{input: "warning", want: slog.LevelWarn},
		{input: "WARNING", want: slog.LevelWarn},
		{input: "error", want: slog.LevelError},
		{input: "ERROR", want: slog.LevelError},
		{input: "", want: slog.LevelInfo},
		{input: "unknown", want: slog.LevelInfo},
		{input: "trace", want: slog.LevelInfo},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseLogLevel(tt.input)
			if got != tt.want {
				t.Errorf("parseLogLevel(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestLoadDefaults(t *testing.T) {
	// Load with no config file — should use all defaults.
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.ListenAddr != ":8080" {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, ":8080")
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %v, want %v", cfg.LogLevel, slog.LevelInfo)
	}
	if cfg.DBMaxConns != 10 {
		t.Errorf("DBMaxConns = %d, want %d", cfg.DBMaxConns, 10)
	}
	if cfg.DBMinConns != 2 {
		t.Errorf("DBMinConns = %d, want %d", cfg.DBMinConns, 2)
	}
	if !cfg.AuthEnabled {
		t.Error("AuthEnabled should be true by default")
	}
	if !cfg.AuthReadEndpoints {
		t.Error("AuthReadEndpoints should be true by default")
	}
	if cfg.NotificationBufferSize != 1024 {
		t.Errorf("NotificationBufferSize = %d, want %d", cfg.NotificationBufferSize, 1024)
	}
	if cfg.NotificationWorkers != 4 {
		t.Errorf("NotificationWorkers = %d, want %d", cfg.NotificationWorkers, 4)
	}
	if cfg.MetricsAddr != "" {
		t.Errorf("MetricsAddr = %q, want empty", cfg.MetricsAddr)
	}
	if cfg.LogShipper.Enabled {
		t.Error("LogShipper.Enabled should be false by default")
	}
	if cfg.LogShipper.Service != "taillight" {
		t.Errorf("LogShipper.Service = %q, want %q", cfg.LogShipper.Service, "taillight")
	}
	if cfg.Analysis.Enabled {
		t.Error("Analysis.Enabled should be false by default")
	}
	if cfg.Analysis.Model != "llama3" {
		t.Errorf("Analysis.Model = %q, want %q", cfg.Analysis.Model, "llama3")
	}
	if cfg.Analysis.Temperature != 0.3 {
		t.Errorf("Analysis.Temperature = %f, want %f", cfg.Analysis.Temperature, 0.3)
	}
	if cfg.Analysis.NumCtx != 8192 {
		t.Errorf("Analysis.NumCtx = %d, want %d", cfg.Analysis.NumCtx, 8192)
	}
	if cfg.Notification.Enabled {
		t.Error("Notification.Enabled should be false by default")
	}
	if cfg.Notification.DispatchWorkers != 4 {
		t.Errorf("Notification.DispatchWorkers = %d, want %d", cfg.Notification.DispatchWorkers, 4)
	}
	if cfg.Notification.DispatchBuffer != 1024 {
		t.Errorf("Notification.DispatchBuffer = %d, want %d", cfg.Notification.DispatchBuffer, 1024)
	}
	if cfg.Notification.DefaultBurstWindow != 30*time.Second {
		t.Errorf("Notification.DefaultBurstWindow = %v, want %v", cfg.Notification.DefaultBurstWindow, 30*time.Second)
	}
	if cfg.Notification.DefaultCooldown != 1*time.Minute {
		t.Errorf("Notification.DefaultCooldown = %v, want %v", cfg.Notification.DefaultCooldown, 1*time.Minute)
	}
	if cfg.Notification.DefaultMaxCooldown != 1*time.Hour {
		t.Errorf("Notification.DefaultMaxCooldown = %v, want %v", cfg.Notification.DefaultMaxCooldown, 1*time.Hour)
	}
	if cfg.Notification.SendTimeout != 10*time.Second {
		t.Errorf("Notification.SendTimeout = %v, want %v", cfg.Notification.SendTimeout, 10*time.Second)
	}
}

func TestLoadEnvOverride(t *testing.T) {
	t.Setenv("LISTEN_ADDR", ":9090")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("AUTH_ENABLED", "false")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.ListenAddr != ":9090" {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, ":9090")
	}
	if cfg.LogLevel != slog.LevelDebug {
		t.Errorf("LogLevel = %v, want %v", cfg.LogLevel, slog.LevelDebug)
	}
	if cfg.AuthEnabled {
		t.Error("AuthEnabled should be false after env override")
	}
}
