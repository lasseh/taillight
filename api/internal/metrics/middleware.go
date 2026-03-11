package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// HTTPMetrics is a chi middleware that records request count and latency.
func HTTPMetrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

		next.ServeHTTP(ww, r)

		// Use the chi route pattern for cardinality control.
		pattern := chi.RouteContext(r.Context()).RoutePattern()
		if pattern == "" {
			pattern = "unknown"
		}

		status := strconv.Itoa(ww.Status())
		HTTPRequestsTotal.WithLabelValues(r.Method, pattern, status).Inc()
		HTTPRequestDuration.WithLabelValues(r.Method, pattern).Observe(time.Since(start).Seconds())
	})
}
