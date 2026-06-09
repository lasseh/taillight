package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lasseh/taillight/internal/config"
)

func TestConfigHandler_Features(t *testing.T) {
	tests := []struct {
		name     string
		cfg      config.FeaturesConfig
		analysis bool
		want     FeaturesResponse
	}{
		{
			name:     "all enabled",
			cfg:      config.FeaturesConfig{Srvlog: true, Netlog: true, AppLog: true},
			analysis: true,
			want:     FeaturesResponse{Srvlog: true, Netlog: true, Applog: true, Analysis: true},
		},
		{
			name: "all disabled",
			cfg:  config.FeaturesConfig{},
			want: FeaturesResponse{},
		},
		{
			name:     "mixed",
			cfg:      config.FeaturesConfig{Srvlog: true, Netlog: false, AppLog: true},
			analysis: false,
			want:     FeaturesResponse{Srvlog: true, Netlog: false, Applog: true, Analysis: false},
		},
		{
			name:     "analysis on, feeds off",
			cfg:      config.FeaturesConfig{},
			analysis: true,
			want:     FeaturesResponse{Analysis: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewConfigHandler(tt.cfg, tt.analysis)

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/config/features", nil)
			w := httptest.NewRecorder()
			h.Features(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
			}
			if ct := w.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q, want %q", ct, "application/json")
			}

			var got FeaturesResponse
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got != tt.want {
				t.Errorf("features = %+v, want %+v", got, tt.want)
			}
		})
	}
}
