package main

import (
	"fmt"
	"os"
	"time"

	"go.yaml.in/yaml/v3"
)

// config holds the top-level YAML configuration for taillight-shipper.
type config struct {
	Endpoint    string       `yaml:"endpoint"`
	APIKey      string       `yaml:"api_key"`
	Service     string       `yaml:"service"`
	Component   string       `yaml:"component"`
	BatchSize   int          `yaml:"batch_size"`
	FlushPeriod string       `yaml:"flush_period"`
	BufferSize  int          `yaml:"buffer_size"`
	Files       []fileConfig `yaml:"files"`
}

// fileConfig describes a single file to tail.
type fileConfig struct {
	Path      string `yaml:"path"`
	Service   string `yaml:"service"`
	Component string `yaml:"component"`
}

// resolvedService returns the file-level service if set, otherwise the
// top-level default.
func (f fileConfig) resolvedService(fallback string) string {
	if f.Service != "" {
		return f.Service
	}
	return fallback
}

// resolvedComponent returns the file-level component if set, otherwise the
// top-level default.
func (f fileConfig) resolvedComponent(fallback string) string {
	if f.Component != "" {
		return f.Component
	}
	return fallback
}

// loadConfig reads and parses the YAML config at path.
func loadConfig(path string) (config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return config{}, fmt.Errorf("read file: %w", err)
	}

	var cfg config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return config{}, fmt.Errorf("parse yaml: %w", err)
	}

	return cfg, nil
}

// parseFlushPeriod parses the flush_period string, defaulting to 1s.
func parseFlushPeriod(s string) (time.Duration, error) {
	if s == "" {
		return time.Second, nil
	}
	return time.ParseDuration(s)
}
