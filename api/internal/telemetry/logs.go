// Package telemetry exports taillight's own logs over OTLP.
//
// It is driven entirely by the standard OpenTelemetry environment variables
// (OTEL_EXPORTER_OTLP_ENDPOINT, OTEL_EXPORTER_OTLP_LOGS_ENDPOINT,
// OTEL_SERVICE_NAME, OTEL_RESOURCE_ATTRIBUTES, ...). With no endpoint set,
// nothing is constructed and nothing dials out.
package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
)

// serviceName is the default service.name; OTEL_SERVICE_NAME overrides it.
const serviceName = "taillight"

// scopeName is the instrumentation scope of every record taillight emits.
const scopeName = "github.com/lasseh/taillight"

// Enabled reports whether the environment asks for OTLP log export: an OTLP
// endpoint is set and the SDK is not disabled.
func Enabled(getenv func(string) string) bool {
	if strings.EqualFold(getenv("OTEL_SDK_DISABLED"), "true") {
		return false
	}
	return getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" ||
		getenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT") != ""
}

// Logs owns the OTLP log pipeline. Handler is the slog.Handler to fan records
// into; Shutdown flushes buffered records and must be called on exit.
type Logs struct {
	Handler  slog.Handler
	provider *sdklog.LoggerProvider
}

// errorInterval is the most often a repeated SDK error, such as every batch
// failing while the collector is down, is reported.
const errorInterval = time.Minute

// NewLogs builds the OTLP/HTTP log pipeline. version becomes service.version,
// and level is the minimum level exported. Records carry the source location
// of the call that logged them. SDK errors, a failed export among them, go to
// errs at most once per errorInterval; errs must not export over OTLP itself.
func NewLogs(ctx context.Context, version string, level slog.Leveler, errs *slog.Logger) (*Logs, error) {
	otel.SetErrorHandler(&errorHandler{logger: errs, interval: errorInterval, now: time.Now})
	exp, err := otlploghttp.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("otlp log exporter: %w", err)
	}
	res, err := resource.New(ctx,
		resource.WithTelemetrySDK(),
		resource.WithHost(),
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion(version),
		),
		// Last, so OTEL_SERVICE_NAME and OTEL_RESOURCE_ATTRIBUTES win.
		resource.WithFromEnv(),
	)
	if err != nil {
		return nil, fmt.Errorf("otel resource: %w", err)
	}
	return newLogs(res, exp, level), nil
}

func newLogs(res *resource.Resource, exp sdklog.Exporter, level slog.Leveler) *Logs {
	provider := sdklog.NewLoggerProvider(
		sdklog.WithResource(res),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exp)),
	)
	h := otelslog.NewHandler(scopeName,
		otelslog.WithLoggerProvider(provider),
		otelslog.WithSource(true),
	)
	return &Logs{Handler: &levelHandler{Handler: h, level: level}, provider: provider}
}

// Shutdown flushes buffered records and stops the exporter. ctx bounds it.
func (l *Logs) Shutdown(ctx context.Context) error {
	if err := l.provider.Shutdown(ctx); err != nil {
		return fmt.Errorf("otel logs shutdown: %w", err)
	}
	return nil
}

// levelHandler drops records below level. The OTel SDK exports every
// severity by default, so the console's level is applied here.
type levelHandler struct {
	slog.Handler
	level slog.Leveler
}

func (h *levelHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return l >= h.level.Level() && h.Handler.Enabled(ctx, l)
}

func (h *levelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &levelHandler{Handler: h.Handler.WithAttrs(attrs), level: h.level}
}

func (h *levelHandler) WithGroup(name string) slog.Handler {
	return &levelHandler{Handler: h.Handler.WithGroup(name), level: h.level}
}

// errorHandler reports OTel SDK errors through a logger instead of the SDK's
// default, which prints each one to stderr with the log package: one raw line
// per failed batch for as long as the collector is down.
type errorHandler struct {
	logger   *slog.Logger
	interval time.Duration
	now      func() time.Time

	mu         sync.Mutex
	last       time.Time
	suppressed int
}

// Handle logs err unless one was logged less than interval ago, in which case
// it is counted and the count goes out with the next one logged.
func (h *errorHandler) Handle(err error) {
	h.mu.Lock()
	now := h.now()
	if !h.last.IsZero() && now.Sub(h.last) < h.interval {
		h.suppressed++
		h.mu.Unlock()
		return
	}
	suppressed := h.suppressed
	h.last, h.suppressed = now, 0
	h.mu.Unlock()

	h.logger.Warn("otel sdk error", "error", err, "suppressed", suppressed)
}
