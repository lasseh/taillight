package config

import (
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
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
	if cfg.DBMaxConns != 30 {
		t.Errorf("DBMaxConns = %d, want %d", cfg.DBMaxConns, 30)
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
	if cfg.Analysis.OllamaTimeout != 2*time.Hour {
		t.Errorf("Analysis.OllamaTimeout = %s, want 2h", cfg.Analysis.OllamaTimeout)
	}
	if cfg.Analysis.RunTimeout != 4*time.Hour {
		t.Errorf("Analysis.RunTimeout = %s, want 4h", cfg.Analysis.RunTimeout)
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
	if cfg.Notification.DefaultSilence != 5*time.Minute {
		t.Errorf("Notification.DefaultSilence = %v, want %v", cfg.Notification.DefaultSilence, 5*time.Minute)
	}
	if cfg.Notification.DefaultSilenceMax != 15*time.Minute {
		t.Errorf("Notification.DefaultSilenceMax = %v, want %v", cfg.Notification.DefaultSilenceMax, 15*time.Minute)
	}
	if cfg.Notification.DefaultCoalesce != 0 {
		t.Errorf("Notification.DefaultCoalesce = %v, want 0", cfg.Notification.DefaultCoalesce)
	}
	if cfg.Notification.SendTimeout != 10*time.Second {
		t.Errorf("Notification.SendTimeout = %v, want %v", cfg.Notification.SendTimeout, 10*time.Second)
	}
}

// TestLoadTrustedProxies verifies trusted_proxies entries parse as CIDRs or
// bare IPs (normalized to single-address prefixes) and that an invalid entry
// fails Load.
func TestLoadTrustedProxies(t *testing.T) {
	dir := t.TempDir()

	path := filepath.Join(dir, "config.yml")
	yml := "trusted_proxies:\n  - \"172.18.0.0/16\"\n  - \"127.0.0.1\"\n"
	if err := os.WriteFile(path, []byte(yml), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := []netip.Prefix{
		netip.MustParsePrefix("172.18.0.0/16"),
		netip.MustParsePrefix("127.0.0.1/32"),
	}
	if !slices.Equal(cfg.TrustedProxies, want) {
		t.Errorf("TrustedProxies = %v, want %v", cfg.TrustedProxies, want)
	}

	bad := filepath.Join(dir, "bad.yml")
	if err := os.WriteFile(bad, []byte("trusted_proxies:\n  - \"not-a-cidr\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(bad); err == nil {
		t.Error("Load() with invalid trusted_proxies entry should fail")
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

// TestLoadNestedSecretEnvOverride verifies dotted secret keys are overridable
// via their flat env vars, as the docs/config example promise (audit S4).
func TestLoadNestedSecretEnvOverride(t *testing.T) {
	t.Setenv("SMTP_PASSWORD", "smtp-secret")
	t.Setenv("NETBOX_TOKEN", "netbox-secret")
	t.Setenv("LDAP_BIND_PASSWORD", "ldap-secret")
	t.Setenv("LOGSHIPPER_API_KEY", "shipper-secret")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	cases := map[string]string{
		"smtp.password":      cfg.SMTP.Password,
		"netbox.token":       cfg.Netbox.Token,
		"ldap.bind_password": cfg.LDAP.BindPassword,
		"logshipper.api_key": cfg.LogShipper.APIKey,
	}
	want := map[string]string{
		"smtp.password":      "smtp-secret",
		"netbox.token":       "netbox-secret",
		"ldap.bind_password": "ldap-secret",
		"logshipper.api_key": "shipper-secret",
	}
	for key, got := range cases {
		if got != want[key] {
			t.Errorf("%s = %q, want %q (env override not applied)", key, got, want[key])
		}
	}
}
