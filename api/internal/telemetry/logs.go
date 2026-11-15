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

	"go.opentelemetry.io/contrib/bridges/otelslog"
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

// NewLogs builds the OTLP/HTTP log pipeline. version becomes service.version,
// and level is the minimum level exported. Records carry the source location
// of the call that logged them.
func NewLogs(ctx context.Context, version string, level slog.Leveler) (*Logs, error) {
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
