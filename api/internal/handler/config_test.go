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
		name string
		cfg  config.FeaturesConfig
		want FeaturesResponse
	}{
		{
			name: "all enabled",
			cfg:  config.FeaturesConfig{Srvlog: true, Netlog: true, AppLog: true},
			want: FeaturesResponse{Srvlog: true, Netlog: true, Applog: true},
		},
		{
			name: "all disabled",
			cfg:  config.FeaturesConfig{},
			want: FeaturesResponse{},
		},
		{
			name: "mixed",
			cfg:  config.FeaturesConfig{Srvlog: true, Netlog: false, AppLog: true},
			want: FeaturesResponse{Srvlog: true, Netlog: false, Applog: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewConfigHandler(tt.cfg)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/config/features", nil)
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
