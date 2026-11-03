package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// methodOther labels every request method outside the standard set.
const methodOther = "OTHER"

// methodLabel folds a request method to a fixed set of label values, so
// request traffic cannot add series. It returns constants, never its argument:
// r.Method is a substring of the request line, and a retained label value
// would keep the whole line in memory.
func methodLabel(m string) string {
	switch m {
	case http.MethodGet:
		return http.MethodGet
	case http.MethodHead:
		return http.MethodHead
	case http.MethodPost:
		return http.MethodPost
	case http.MethodPut:
		return http.MethodPut
	case http.MethodPatch:
		return http.MethodPatch
	case http.MethodDelete:
		return http.MethodDelete
	case http.MethodConnect:
		return http.MethodConnect
	case http.MethodOptions:
		return http.MethodOptions
	case http.MethodTrace:
		return http.MethodTrace
	default:
		return methodOther
	}
}

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

		method := methodLabel(r.Method)
		status := strconv.Itoa(ww.Status())
		HTTPRequestsTotal.WithLabelValues(method, pattern, status).Inc()
		HTTPRequestDuration.WithLabelValues(method, pattern).Observe(time.Since(start).Seconds())
	})
}
