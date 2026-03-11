package handler

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"
)

type ctxKeyLogger struct{}

// RequestLogger is a middleware that creates a request-scoped logger with the
// chi request ID and stores it in the request context.
func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := middleware.GetReqID(r.Context())
		logger := slog.Default().With("request_id", reqID)
		ctx := context.WithValue(r.Context(), ctxKeyLogger{}, logger)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// LoggerFromContext returns the request-scoped logger, or slog.Default() if none.
func LoggerFromContext(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(ctxKeyLogger{}).(*slog.Logger); ok {
		return l
	}
	return slog.Default()
}

// SkipPath wraps a middleware so it is bypassed for the given path.
// Requests to that path go straight to the next handler without the middleware.
func SkipPath(mw func(http.Handler) http.Handler, path string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		wrapped := mw(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == path {
				next.ServeHTTP(w, r)
				return
			}
			wrapped.ServeHTTP(w, r)
		})
	}
}
