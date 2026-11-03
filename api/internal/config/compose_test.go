package config

import (
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// Repository files the compose tests read, relative to this package.
const (
	composeFile    = "../../../docker-compose.yml"
	envExampleFile = "../../../.env.example"
)

// envTableDocs are the docs whose .env table lists .env variables, beside
// .env.example itself.
var envTableDocs = []string{"../../../README.md", "../../../docs/OVERVIEW.md"}

// composeOnlyVars are documented .env variables that configure compose or
// the frontend, not an API setting. POSTGRES_PASSWORD reaches the API only
// inside DATABASE_URL.
var composeOnlyVars = map[string]bool{
	"POSTGRES_PASSWORD":   true,
	"POSTGRES_BIND":       true,
	"API_HOST_PORT":       true,
	"FRONTEND_HOST_PORT":  true,
	"RSYSLOG_NETLOG_PORT": true,
	"RSYSLOG_SRVLOG_PORT": true,
	"API_URL":             true,
}

var (
	envLine  = regexp.MustCompile(`^([A-Z][A-Z0-9_]*)=(.*)$`)
	tableRow = regexp.MustCompile("^\\| `([A-Z][A-Z0-9_]*)` \\|")
	// composeVar matches ${NAME}, ${NAME:-default}, ${NAME-default},
	// ${NAME:?message} and ${NAME?message}.
	composeVar = regexp.MustCompile(`\$\{([A-Z][A-Z0-9_]*)(?:(:?[-?])([^}]*))?\}`)
)

// apiEnvironment returns services.api.environment from docker-compose.yml,
// before interpolation.
func apiEnvironment(t *testing.T) map[string]string {
	t.Helper()
	data, err := os.ReadFile(composeFile)
	if err != nil {
		t.Fatal(err)
	}
	var compose struct {
		Services map[string]struct {
			Environment map[string]string `yaml:"environment"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(data, &compose); err != nil {
		t.Fatalf("parse %s: %v", composeFile, err)
	}
	env := compose.Services["api"].Environment
	if len(env) == 0 {
		t.Fatalf("%s: services.api.environment is empty", composeFile)
	}
	return env
}

// documentedEnvVars returns every variable .env.example sets or a README or
// overview .env table lists, with the file that documents it.
func documentedEnvVars(t *testing.T) map[string]string {
	t.Helper()
	vars := map[string]string{}
	for name := range readEnvExample(t) {
		vars[name] = envExampleFile
	}
	for _, path := range envTableDocs {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		inEnvTable := false
		for line := range strings.Lines(string(data)) {
			if strings.HasPrefix(line, "#") {
				inEnvTable = strings.Contains(line, "`.env`")
				continue
			}
			if m := tableRow.FindStringSubmatch(line); inEnvTable && m != nil {
				vars[m[1]] = path
			}
		}
	}
	return vars
}

// readEnvExample returns the variables .env.example sets, with their values.
func readEnvExample(t *testing.T) map[string]string {
	t.Helper()
	data, err := os.ReadFile(envExampleFile)
	if err != nil {
		t.Fatal(err)
	}
	vars := map[string]string{}
	for line := range strings.Lines(string(data)) {
		if m := envLine.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			vars[m[1]] = m[2]
		}
	}
	return vars
}

// interpolate expands compose variable references in s from dotenv the way
// compose does: a ':' form treats an empty value as unset, '-' supplies a
// default and '?' refuses to start.
func interpolate(t *testing.T, s string, dotenv map[string]string) string {
	t.Helper()
	return composeVar.ReplaceAllStringFunc(s, func(ref string) string {
		m := composeVar.FindStringSubmatch(ref)
		name, op, arg := m[1], m[2], m[3]
		val, set := dotenv[name]
		if strings.HasPrefix(op, ":") && val == "" {
			set = false
		}
		switch {
		case set:
			return val
		case strings.HasSuffix(op, "?"):
			t.Fatalf("compose refuses to start: %s is not set (%s)", name, arg)
		}
		return arg
	})
}

// TestComposeAPIEnvironmentCoversDocumentedEnvVars verifies every API setting
// documented for .env is passed to the api service and takes its value from
// the .env variable of the same name. Compose reads .env only to interpolate
// the compose file, so a variable missing here never reaches the API.
func TestComposeAPIEnvironmentCoversDocumentedEnvVars(t *testing.T) {
	apiEnv := apiEnvironment(t)
	documented := documentedEnvVars(t)
	for _, name := range []string{"AUTH_ENABLED", "REAL_IP_HEADER", "TRUSTED_PROXIES"} {
		if documented[name] == "" {
			t.Errorf("%s is not documented for .env; the doc parser may have missed a table", name)
		}
	}
	for name, doc := range documented {
		if composeOnlyVars[name] {
			continue
		}
		val, ok := apiEnv[name]
		if !ok {
			t.Errorf("%s is documented for .env in %s but missing from services.api.environment", name, doc)
			continue
		}
		if !strings.Contains(val, "${"+name) {
			t.Errorf("services.api.environment %s = %q does not take its value from .env", name, val)
		}
	}
}

// TestComposeAPIEnvironmentReachesLoad renders services.api.environment from
// .env.example and loads config under it. The example files load as shipped,
// an empty proxy setting leaves the config file value in force, a set one
// reaches the config, and TRUSTED_PROXIES splits on spaces, not commas.
func TestComposeAPIEnvironmentReachesLoad(t *testing.T) {
	example, err := os.ReadFile("../../config.yml.example")
	if err != nil {
		t.Fatal(err)
	}
	proxyYML := "real_ip_header: X-Forwarded-For\ntrusted_proxies:\n  - \"10.1.0.0/16\"\n"
	tests := []struct {
		name        string
		set         map[string]string
		yml         string
		wantHeader  string
		wantProxies []netip.Prefix
		wantErr     bool
	}{
		{
			name: "example files as shipped",
			yml:  string(example),
		},
		{
			name:        "empty proxy settings keep the file values",
			yml:         proxyYML,
			wantHeader:  "X-Forwarded-For",
			wantProxies: []netip.Prefix{netip.MustParsePrefix("10.1.0.0/16")},
		},
		{
			name:        "proxy settings set in .env",
			set:         map[string]string{"REAL_IP_HEADER": "X-Real-IP", "TRUSTED_PROXIES": "172.18.0.0/16 127.0.0.1"},
			yml:         proxyYML,
			wantHeader:  "X-Real-IP",
			wantProxies: []netip.Prefix{netip.MustParsePrefix("172.18.0.0/16"), netip.MustParsePrefix("127.0.0.1/32")},
		},
		{
			name:    "comma separated proxies stop startup",
			set:     map[string]string{"REAL_IP_HEADER": "X-Real-IP", "TRUSTED_PROXIES": "172.18.0.0/16,127.0.0.1"},
			yml:     proxyYML,
			wantErr: true,
		},
	}
	apiEnv := apiEnvironment(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yml")
			if err := os.WriteFile(path, []byte(tt.yml), 0o600); err != nil {
				t.Fatal(err)
			}
			dotenv := readEnvExample(t)
			for k, v := range tt.set {
				dotenv[k] = v
			}
			for k, v := range apiEnv {
				t.Setenv(k, interpolate(t, v, dotenv))
			}
			cfg, err := Load(path)
			if tt.wantErr {
				if err == nil {
					t.Fatal("Load() under the rendered api environment: want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() under the rendered api environment: %v", err)
			}
			if cfg.RealIPHeader != tt.wantHeader {
				t.Errorf("RealIPHeader = %q, want %q", cfg.RealIPHeader, tt.wantHeader)
			}
			if !slices.Equal(cfg.TrustedProxies, tt.wantProxies) {
				t.Errorf("TrustedProxies = %v, want %v", cfg.TrustedProxies, tt.wantProxies)
			}
		})
	}
}
