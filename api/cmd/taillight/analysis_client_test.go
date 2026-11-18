package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/lasseh/taillight/internal/config"
	"github.com/lasseh/taillight/internal/ollama"
	"github.com/lasseh/taillight/internal/openai"
)

// TestNewAnalysisClient covers the backend choice at startup: analysis.api
// picks the client, and a configuration that cannot work is an error that
// never prints the key.
func TestNewAnalysisClient(t *testing.T) {
	const key = "sk-secret-0123456789"
	gateway := config.AnalysisConfig{Enabled: true, API: "openai", APIURL: "https://gateway.example", APIKey: key}
	with := func(edit func(*config.AnalysisConfig)) config.AnalysisConfig {
		c := gateway
		edit(&c)
		return c
	}
	tests := []struct {
		name    string
		cfg     config.AnalysisConfig
		want    string // "ollama" or "openai"; empty means an error.
		wantErr string
	}{
		{"ollama", config.AnalysisConfig{Enabled: true, API: "ollama", OllamaURL: "http://localhost:11434"}, "ollama", ""},
		{"openai", gateway, "openai", ""},
		{"openai without key", with(func(c *config.AnalysisConfig) { c.APIKey = "" }), "", "analysis.api_key"},
		{"openai without url", with(func(c *config.AnalysisConfig) { c.APIURL = "" }), "", "analysis.api_url"},
		{"openai with unreadable ca_file", with(func(c *config.AnalysisConfig) { c.CAFile = filepath.Join(t.TempDir(), "absent.pem") }), "", "ca_file"},
		{"unknown api", with(func(c *config.AnalysisConfig) { c.API = "anthropic" }), "", `"anthropic"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := newAnalysisClient(tt.cfg)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatal("newAnalysisClient: want error")
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error = %q, want it to name %s", err, tt.wantErr)
				}
				if strings.Contains(err.Error(), key) {
					t.Errorf("error prints the key: %q", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("newAnalysisClient: %v", err)
			}
			var got string
			switch client.(type) {
			case *ollama.Client:
				got = "ollama"
			case *openai.Client:
				got = "openai"
			}
			if got != tt.want {
				t.Errorf("client is %T, want the %s client", client, tt.want)
			}
		})
	}
}
