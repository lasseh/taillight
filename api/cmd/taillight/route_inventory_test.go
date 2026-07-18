package main

import (
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/go-chi/chi/v5"
	"github.com/lasseh/taillight/docs"
	"github.com/lasseh/taillight/internal/broker"
	"github.com/lasseh/taillight/internal/config"
	"github.com/lasseh/taillight/internal/handler"
	"github.com/lasseh/taillight/internal/netbox"
	oidcauth "github.com/lasseh/taillight/internal/oidc"
	"github.com/lasseh/taillight/internal/postgres"
)

// routeExclusions are router entries that intentionally have no OpenAPI
// entry: the endpoints that deliver the docs themselves. Keyed by the full
// chi route pattern. Anything else registered without a spec entry fails
// the inventory test.
var routeExclusions = map[string]bool{
	"/api/v1/openapi.yml": true,
	"/api/docs":           true,
}

// buildFullRouter constructs the production router via setupRouter with every
// conditional route group enabled (auth, netbox enrichment, analysis) and
// inert stub dependencies. Route registration never touches the database or
// the network, so zero-value stores and nil engines are safe here.
func buildFullRouter() chi.Router {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store := &postgres.Store{}
	authStore := &postgres.AuthStore{}
	cfg := config.Config{AuthEnabled: true}
	analysis := &analysisWiring{
		reports:   handler.NewAnalysisHandler(store, nil),
		schedules: handler.NewAnalysisScheduleHandler(store, nil),
	}
	return setupRouter(cfg, logger, store, authStore, nil, oidcauth.New(oidcauth.Config{}, logger),
		broker.NewSrvlogBroker(logger), broker.NewNetlogBroker(logger), broker.NewAppLogBroker(logger),
		analysis, nil, nil, &netbox.Client{})
}

// specPaths parses the embedded openapi.yml into path -> set of lowercase
// HTTP methods.
func specPaths(t *testing.T) map[string]map[string]bool {
	t.Helper()
	var spec struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(docs.Spec(), &spec); err != nil {
		t.Fatalf("parse embedded openapi.yml: %v", err)
	}
	if len(spec.Paths) == 0 {
		t.Fatal("embedded openapi.yml has no paths")
	}
	out := make(map[string]map[string]bool, len(spec.Paths))
	for path, item := range spec.Paths {
		methods := make(map[string]bool)
		for key := range item {
			switch key {
			case "get", "post", "put", "patch", "delete", "head", "options":
				methods[key] = true
			}
		}
		out[path] = methods
	}
	return out
}

// specPathFor maps a chi route pattern to its OpenAPI path. The spec's
// server URL is /api/v1, so spec paths are relative to that prefix; /health
// is the one route outside it (its path entry carries a servers override).
// Returns "" for routes that have no spec-relative form.
func specPathFor(route string) string {
	if route == "/health" {
		return "/health"
	}
	if rest, ok := strings.CutPrefix(route, "/api/v1"); ok {
		rest = strings.TrimSuffix(rest, "/")
		if rest == "" {
			rest = "/"
		}
		return rest
	}
	return ""
}

// TestRouteInventoryMatchesOpenAPISpec walks the real chi routing table (all
// conditional groups enabled) and asserts it matches the embedded OpenAPI
// spec in both directions: every registered route+method has a spec entry,
// and every spec entry has a registered route. A new route without a spec
// entry — or a spec entry for a route that no longer exists — fails CI.
func TestRouteInventoryMatchesOpenAPISpec(t *testing.T) {
	spec := specPaths(t)
	router := buildFullRouter()

	// route pattern -> methods actually registered, spec-path keyed.
	registered := make(map[string]map[string]bool)
	err := chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if routeExclusions[route] {
			return nil
		}
		specPath := specPathFor(route)
		if specPath == "" {
			t.Errorf("route %s %s is outside /api/v1 and not a known exception; document it in openapi.yml or exclude it here", method, route)
			return nil
		}
		if registered[specPath] == nil {
			registered[specPath] = make(map[string]bool)
		}
		registered[specPath][strings.ToLower(method)] = true
		return nil
	})
	if err != nil {
		t.Fatalf("walk router: %v", err)
	}

	// Every registered route+method must have a spec entry.
	for path, methods := range registered {
		for method := range methods {
			if !spec[path][method] {
				t.Errorf("route %s %s is registered but missing from docs/openapi.yml", strings.ToUpper(method), path)
			}
		}
	}

	// Every spec entry must correspond to a registered route (no phantom docs).
	for path, methods := range spec {
		for method := range methods {
			if !registered[path][method] {
				t.Errorf("docs/openapi.yml documents %s %s but no such route is registered", strings.ToUpper(method), path)
			}
		}
	}
}
