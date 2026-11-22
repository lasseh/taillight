package telemetry

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
)

func TestEnabled(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want bool
	}{
		{name: "nothing set", env: nil, want: false},
		{name: "generic endpoint", env: map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "http://127.0.0.1:4318"}, want: true},
		{name: "logs endpoint", env: map[string]string{"OTEL_EXPORTER_OTLP_LOGS_ENDPOINT": "http://127.0.0.1:4318/v1/logs"}, want: true},
		{name: "sdk disabled wins", env: map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "http://127.0.0.1:4318", "OTEL_SDK_DISABLED": "TRUE"}, want: false},
		{name: "only unrelated otel vars", env: map[string]string{"OTEL_SERVICE_NAME": "x"}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getenv := func(k string) string { return tt.env[k] }
			if got := Enabled(getenv); got != tt.want {
				t.Errorf("Enabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

// memExporter records exported log records.
type memExporter struct {
	mu      sync.Mutex
	records []sdklog.Record
}

func (e *memExporter) Export(_ context.Context, records []sdklog.Record) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, r := range records {
		e.records = append(e.records, r.Clone())
	}
	return nil
}

func (e *memExporter) Shutdown(context.Context) error   { return nil }
func (e *memExporter) ForceFlush(context.Context) error { return nil }

func TestLogsExportsSourceVersionAndLevel(t *testing.T) {
	exp := &memExporter{}
	res := resource.NewSchemaless(semconv.ServiceName(serviceName), semconv.ServiceVersion("v1.2.3"))
	logs := newLogs(res, exp, slog.LevelWarn)

	logger := slog.New(logs.Handler).With("component", "test")
	logger.Info("dropped below level")
	logger.Error("boom", "err", "disk full")

	if err := logs.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	if len(exp.records) != 1 {
		t.Fatalf("exported %d records, want 1 (info must be filtered)", len(exp.records))
	}
	r := exp.records[0]
	if got := r.Body().AsString(); got != "boom" {
		t.Errorf("body = %q, want %q", got, "boom")
	}
	if got := r.SeverityText(); got != "ERROR" {
		t.Errorf("severity = %q, want ERROR", got)
	}

	attrs := map[string]string{}
	r.WalkAttributes(func(kv attribute.KeyValue) bool {
		attrs[string(kv.Key)] = kv.Value.String()
		return true
	})
	if !strings.HasSuffix(attrs[string(semconv.CodeFilePathKey)], "logs_test.go") {
		t.Errorf("code.file.path = %q, want the calling test file", attrs[string(semconv.CodeFilePathKey)])
	}
	if attrs[string(semconv.CodeLineNumberKey)] == "" {
		t.Error("code.line.number missing")
	}
	if attrs["component"] != "test" || attrs["err"] != "disk full" {
		t.Errorf("attributes = %v, want component and err carried", attrs)
	}

	var version string
	for _, kv := range r.Resource().Attributes() {
		if kv.Key == semconv.ServiceVersionKey {
			version = kv.Value.AsString()
		}
	}
	if version != "v1.2.3" {
		t.Errorf("service.version = %q, want v1.2.3", version)
	}
}

// A collector that is down fails every batch. The handler reports the first
// failure, then at most one per interval with the count it held back.
func TestErrorHandlerRateLimits(t *testing.T) {
	var buf strings.Builder
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	h := &errorHandler{
		logger:   slog.New(slog.NewTextHandler(&buf, nil)),
		interval: time.Minute,
		now:      func() time.Time { return now },
	}

	h.Handle(errors.New("connection refused"))
	h.Handle(errors.New("connection refused"))
	h.Handle(errors.New("connection refused"))
	now = now.Add(time.Minute)
	h.Handle(errors.New("connection refused"))

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("logged %d lines, want 2:\n%s", len(lines), buf.String())
	}
	if !strings.Contains(lines[0], "level=WARN") || !strings.Contains(lines[0], "connection refused") ||
		!strings.Contains(lines[0], "suppressed=0") {
		t.Errorf("first line = %q, want a WARN with the error and suppressed=0", lines[0])
	}
	if !strings.Contains(lines[1], "suppressed=2") {
		t.Errorf("second line = %q, want suppressed=2", lines[1])
	}
}
