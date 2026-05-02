package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadConfig_Valid(t *testing.T) {
	content := `
endpoint: http://localhost:8080/api/v1/applog/ingest
service: myapp
files:
  - path: /var/log/app.log
`
	path := writeTemp(t, content)
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Endpoint != "http://localhost:8080/api/v1/applog/ingest" {
		t.Errorf("endpoint = %q, want %q", cfg.Endpoint, "http://localhost:8080/api/v1/applog/ingest")
	}
	if cfg.Service != "myapp" {
		t.Errorf("service = %q, want %q", cfg.Service, "myapp")
	}
	if len(cfg.Files) != 1 {
		t.Fatalf("files = %d, want 1", len(cfg.Files))
	}
	if cfg.Files[0].Path != "/var/log/app.log" {
		t.Errorf("files[0].path = %q, want %q", cfg.Files[0].Path, "/var/log/app.log")
	}
}

func TestLoadConfig_MissingFile(t *testing.T) {
	_, err := loadConfig("/nonexistent/path/config.yml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadConfig_InvalidYAML(t *testing.T) {
	path := writeTemp(t, "{{invalid yaml")
	_, err := loadConfig(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestValidate_MissingEndpoint(t *testing.T) {
	path := writeTemp(t, `
service: myapp
`)
	_, err := loadConfig(path)
	if err == nil {
		t.Fatal("expected error for missing endpoint")
	}
}

func TestValidate_MissingService(t *testing.T) {
	path := writeTemp(t, `
endpoint: http://localhost:8080/api/v1/applog/ingest
files:
  - path: /var/log/app.log
`)
	_, err := loadConfig(path)
	if err == nil {
		t.Fatal("expected error when service is missing at top-level and file entry")
	}
}

func TestValidate_ServiceOnFileEntry(t *testing.T) {
	path := writeTemp(t, `
endpoint: http://localhost:8080/api/v1/applog/ingest
files:
  - path: /var/log/app.log
    service: myapp
`)
	_, err := loadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_EmptyFilePath(t *testing.T) {
	path := writeTemp(t, `
endpoint: http://localhost:8080/api/v1/applog/ingest
service: myapp
files:
  - path: ""
`)
	_, err := loadConfig(path)
	if err == nil {
		t.Fatal("expected error for empty file path")
	}
}

func TestValidate_NoFiles(t *testing.T) {
	// Config with no files is valid (stdin-only mode).
	path := writeTemp(t, `
endpoint: http://localhost:8080/api/v1/applog/ingest
service: myapp
`)
	_, err := loadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseFlushPeriod(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    time.Duration
		wantErr bool
	}{
		{name: "empty defaults to 1s", input: "", want: time.Second},
		{name: "valid duration", input: "5s", want: 5 * time.Second},
		{name: "valid ms", input: "500ms", want: 500 * time.Millisecond},
		{name: "invalid string", input: "not-a-duration", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseFlushPeriod(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResolvedService(t *testing.T) {
	tests := []struct {
		name     string
		fc       fileConfig
		fallback string
		want     string
	}{
		{name: "file-level set", fc: fileConfig{Service: "override"}, fallback: "default", want: "override"},
		{name: "fallback used", fc: fileConfig{}, fallback: "default", want: "default"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.fc.resolvedService(tt.fallback); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolvedComponent(t *testing.T) {
	tests := []struct {
		name     string
		fc       fileConfig
		fallback string
		want     string
	}{
		{name: "file-level set", fc: fileConfig{Component: "web"}, fallback: "api", want: "web"},
		{name: "fallback used", fc: fileConfig{}, fallback: "api", want: "api"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.fc.resolvedComponent(tt.fallback); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolvedHost(t *testing.T) {
	tests := []struct {
		name     string
		fc       fileConfig
		fallback string
		want     string
	}{
		{name: "file-level set", fc: fileConfig{Host: "node-1"}, fallback: "default-host", want: "node-1"},
		{name: "fallback used", fc: fileConfig{}, fallback: "default-host", want: "default-host"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.fc.resolvedHost(tt.fallback); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}
