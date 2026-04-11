package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/lasseh/taillight/internal/tui"
)

// Config holds all TUI configuration.
type Config struct {
	Server  ServerConfig  `yaml:"server"`
	Display DisplayConfig `yaml:"display"`
}

// ServerConfig holds API connection settings.
type ServerConfig struct {
	URL           string `yaml:"url"`
	APIKey        string `yaml:"api_key"`
	TLSSkipVerify bool   `yaml:"tls_skip_verify"`
}

// DisplayConfig holds rendering preferences.
type DisplayConfig struct {
	FPS             int    `yaml:"fps"`
	BufferSize      int    `yaml:"buffer_size"`
	BatchIntervalMs int    `yaml:"batch_interval_ms"`
	AutoScroll      bool   `yaml:"auto_scroll"`
	TimeFormat      string `yaml:"time_format"`
}

// ToAppConfig converts the file config to the App's internal config.
func (c *Config) ToAppConfig() tui.Config {
	return tui.Config{
		BufferSize:    c.Display.BufferSize,
		BatchInterval: time.Duration(c.Display.BatchIntervalMs) * time.Millisecond,
		AutoScroll:    c.Display.AutoScroll,
		TimeFormat:    c.Display.TimeFormat,
	}
}

func defaultConfig() Config {
	return Config{
		Display: DisplayConfig{
			FPS:             30,
			BufferSize:      10000,
			BatchIntervalMs: 50,
			AutoScroll:      true,
			TimeFormat:      "15:04:05",
		},
	}
}

func loadConfig(path string) (*Config, error) {
	cfg := defaultConfig()

	if path == "" {
		path = defaultConfigPath()
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// No config file — return defaults, user must supply flags.
			return &cfg, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &cfg, nil
}

func defaultConfigPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "taillight", "tui.yml")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "taillight", "tui.yml")
}
