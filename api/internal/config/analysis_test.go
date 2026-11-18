package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadAnalysisAPIDefaults(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	a := cfg.Analysis
	if a.API != AnalysisAPIOllama {
		t.Errorf("Analysis.API = %q, want %q", a.API, AnalysisAPIOllama)
	}
	if a.APIURL != "" || a.APIKey != "" || a.CAFile != "" {
		t.Errorf("Analysis APIURL/APIKey/CAFile = %q/%q/%q, want all empty", a.APIURL, a.APIKey, a.CAFile)
	}
}

func TestLoadAnalysisAPIFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	yml := `analysis:
  enabled: true
  api: openai
  api_url: "https://gateway.example"
  api_key: "sk-from-file"
  ca_file: "/etc/taillight/ca.pem"
  model: general
`
	if err := os.WriteFile(path, []byte(yml), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	a := cfg.Analysis
	if a.API != AnalysisAPIOpenAI || a.APIURL != "https://gateway.example" || a.APIKey != "sk-from-file" || a.CAFile != "/etc/taillight/ca.pem" {
		t.Errorf("Analysis = api %q url %q key %q ca %q, want the file values", a.API, a.APIURL, a.APIKey, a.CAFile)
	}
	if err := a.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}
}

// TestLoadAnalysisAPIKeyEnvOverride covers the key coming from the
// environment, with and without an entry in the file.
func TestLoadAnalysisAPIKeyEnvOverride(t *testing.T) {
	tests := []struct {
		name string
		yml  string
	}{
		{"no file entry", "analysis:\n  api: openai\n"},
		{"over the file entry", "analysis:\n  api: openai\n  api_key: sk-from-file\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yml")
			if err := os.WriteFile(path, []byte(tt.yml), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("ANALYSIS_API_KEY", "sk-from-env")
			cfg, err := Load(path)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if cfg.Analysis.APIKey != "sk-from-env" {
				t.Errorf("Analysis.APIKey = %q, want the env value", cfg.Analysis.APIKey)
			}
		})
	}
}

func TestAnalysisConfigValidate(t *testing.T) {
	const key = "sk-secret-0123456789"
	tests := []struct {
		name    string
		cfg     AnalysisConfig
		wantErr []string // substrings of the error; nil means no error.
	}{
		{"ollama", AnalysisConfig{Enabled: true, API: "ollama"}, nil},
		{"openai complete", AnalysisConfig{Enabled: true, API: "openai", APIURL: "https://gateway.example", APIKey: key}, nil},
		{"openai without url", AnalysisConfig{Enabled: true, API: "openai", APIKey: key}, []string{"analysis.api_url"}},
		{"openai without key", AnalysisConfig{Enabled: true, API: "openai", APIURL: "https://gateway.example"}, []string{"analysis.api_key"}},
		{"openai without either", AnalysisConfig{Enabled: true, API: "openai"}, []string{"analysis.api_url", "analysis.api_key"}},
		{"unknown api", AnalysisConfig{Enabled: true, API: "anthropic", APIKey: key}, []string{`"anthropic"`, `"ollama"`, `"openai"`}},
		{"empty api", AnalysisConfig{Enabled: true}, []string{"analysis.api"}},
		{"wrong case", AnalysisConfig{Enabled: true, API: "OpenAI", APIKey: key}, []string{`"OpenAI"`}},
		{"disabled is not checked", AnalysisConfig{API: "anthropic"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatal("Validate() = nil, want error")
			}
			for _, want := range tt.wantErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("Validate() = %q, want it to name %s", err, want)
				}
			}
			if strings.Contains(err.Error(), key) {
				t.Errorf("Validate() prints the key: %q", err)
			}
		})
	}
}
