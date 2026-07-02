package handler

import (
	"net/http"
)

// ConfigHandler exposes read-only runtime configuration to the frontend.
type ConfigHandler struct {
	features FeaturesResponse
}

// FeaturesResponse is the GET /config/features payload. JSON tags use the
// same lowercase names the frontend has consumed since the static
// `frontend/src/config.ts` implementation.
//
// The three feed keys are always true — feeds are no longer gated — and are
// kept only so the response shape stays stable for the frontend. Analysis is
// the one real flag (config.Analysis.Enabled); it ships here so the frontend
// can hide the analysis nav when the feature is off without a separate request.
type FeaturesResponse struct {
	Netlog   bool `json:"netlog"`
	Srvlog   bool `json:"srvlog"`
	Applog   bool `json:"applog"`
	Analysis bool `json:"analysis"`
}

// NewConfigHandler creates a ConfigHandler with a cached snapshot of the
// feature flags. Flags are set at startup and don't change at runtime,
// so we copy once to avoid re-reading config on every request.
func NewConfigHandler(analysisEnabled bool) *ConfigHandler {
	return &ConfigHandler{
		features: FeaturesResponse{
			Netlog:   true,
			Srvlog:   true,
			Applog:   true,
			Analysis: analysisEnabled,
		},
	}
}

// Features handles GET /api/v1/config/features.
// Returns the feature flags the frontend uses for routing and nav. Served
// unauthenticated — flag state is not secret and the frontend must fetch it
// before any auth UI can render.
func (h *ConfigHandler) Features(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, h.features)
}
