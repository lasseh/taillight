package metrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
)

func TestMethodLabel(t *testing.T) {
	tests := []struct {
		method string
		want   string
	}{
		{http.MethodGet, "GET"},
		{http.MethodHead, "HEAD"},
		{http.MethodPost, "POST"},
		{http.MethodPut, "PUT"},
		{http.MethodPatch, "PATCH"},
		{http.MethodDelete, "DELETE"},
		{http.MethodConnect, "CONNECT"},
		{http.MethodOptions, "OPTIONS"},
		{http.MethodTrace, "TRACE"},
		{"get", "OTHER"},
		{"PROPFIND", "OTHER"},
		{strings.Repeat("X", 4096), "OTHER"},
	}
	for _, tt := range tests {
		t.Run(tt.want+"/"+tt.method[:min(len(tt.method), 16)], func(t *testing.T) {
			if got := methodLabel(tt.method); got != tt.want {
				t.Errorf("methodLabel(%.16q) = %q, want %q", tt.method, got, tt.want)
			}
		})
	}
}

// methodLabels gathers the method label of every series in the named family.
func methodLabels(t *testing.T, family string) []string {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	var out []string
	for _, mf := range families {
		if mf.GetName() != family {
			continue
		}
		for _, m := range mf.GetMetric() {
			for _, lp := range m.GetLabel() {
				if lp.GetName() == "method" {
					out = append(out, lp.GetValue())
				}
			}
		}
	}
	return out
}

func TestHTTPMetricsDistinctMethodsShareOneSeries(t *testing.T) {
	HTTPRequestsTotal.Reset()
	HTTPRequestDuration.Reset()
	t.Cleanup(func() {
		HTTPRequestsTotal.Reset()
		HTTPRequestDuration.Reset()
	})

	r := chi.NewRouter()
	r.Use(HTTPMetrics)
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	for i := range 1000 {
		req := httptest.NewRequestWithContext(context.Background(), "M"+strconv.Itoa(i), "/health", nil)
		r.ServeHTTP(httptest.NewRecorder(), req)
	}

	for _, family := range []string{"taillight_http_requests_total", "taillight_http_request_duration_seconds"} {
		got := methodLabels(t, family)
		if len(got) != 1 || got[0] != "OTHER" {
			t.Errorf("%s method labels = %v, want one OTHER series", family, got)
		}
	}

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/health", nil))
	got := methodLabels(t, "taillight_http_requests_total")
	if len(got) != 2 || !slices.Contains(got, "GET") {
		t.Errorf("method labels after a GET = %v, want OTHER and GET", got)
	}
}
