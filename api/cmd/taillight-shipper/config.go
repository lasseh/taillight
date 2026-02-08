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
	Host        string       `yaml:"host"`
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
	Host      string `yaml:"host"`
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

// resolvedHost returns the file-level host if set, otherwise the top-level
// default.
func (f fileConfig) resolvedHost(fallback string) string {
	if f.Host != "" {
		return f.Host
	}
	return fallback
}

// validate checks that required config fields are present.
func (c config) validate() error {
	if c.Endpoint == "" {
		return fmt.Errorf("endpoint is required")
	}
	for i, f := range c.Files {
		if f.Path == "" {
			return fmt.Errorf("files[%d]: path is required", i)
		}
		if c.Service == "" && f.Service == "" {
			return fmt.Errorf("files[%d]: service must be set at top-level or on the file entry", i)
		}
	}
	return nil
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

	if err := cfg.validate(); err != nil {
		return config{}, fmt.Errorf("validate config: %w", err)
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
