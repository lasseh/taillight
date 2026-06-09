package handler

import (
	"net/http"

	"github.com/lasseh/taillight/internal/config"
)

// ConfigHandler exposes read-only runtime configuration to the frontend.
// Feature flags ship from this endpoint so that enabling or disabling a
// feed doesn't require a frontend rebuild or git diff.
type ConfigHandler struct {
	features FeaturesResponse
}

// FeaturesResponse mirrors config.FeaturesConfig for the HTTP API.
// JSON tags use the same lowercase names the frontend has consumed since
// the static `frontend/src/config.ts` implementation.
//
// Analysis is not part of config.FeaturesConfig (it lives in config.Analysis),
// but it ships here too so the frontend can hide the analysis nav when the
// feature is off without a separate request.
type FeaturesResponse struct {
	Netlog   bool `json:"netlog"`
	Srvlog   bool `json:"srvlog"`
	Applog   bool `json:"applog"`
	Analysis bool `json:"analysis"`
}

// NewConfigHandler creates a ConfigHandler with a cached snapshot of the
// feature flags. Features are set at startup and don't change at runtime,
// so we copy once to avoid re-reading config on every request.
func NewConfigHandler(f config.FeaturesConfig, analysisEnabled bool) *ConfigHandler {
	return &ConfigHandler{
		features: FeaturesResponse{
			Netlog:   f.Netlog,
			Srvlog:   f.Srvlog,
			Applog:   f.AppLog,
			Analysis: analysisEnabled,
		},
	}
}

// Features handles GET /api/v1/config/features.
// Returns the current feature flags so the frontend can render only enabled
// feeds. Served unauthenticated — flag state is not secret and the frontend
// must fetch it before any auth UI can render.
func (h *ConfigHandler) Features(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, h.features)
}
