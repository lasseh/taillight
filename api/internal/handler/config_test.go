package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestConfigHandler_Features(t *testing.T) {
	tests := []struct {
		name     string
		analysis bool
		oidc     bool
		want     FeaturesResponse
	}{
		{
			name:     "analysis enabled",
			analysis: true,
			want:     FeaturesResponse{Srvlog: true, Netlog: true, Applog: true, Analysis: true},
		},
		{
			name:     "analysis disabled",
			analysis: false,
			want:     FeaturesResponse{Srvlog: true, Netlog: true, Applog: true, Analysis: false},
		},
		{
			name: "oidc enabled",
			oidc: true,
			want: FeaturesResponse{Srvlog: true, Netlog: true, Applog: true, OIDC: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewConfigHandler(tt.analysis, tt.oidc)

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
