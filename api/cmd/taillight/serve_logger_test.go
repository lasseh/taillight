package main

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/lasseh/taillight/internal/config"
)

// OTLP export must stay off unless an endpoint is set, so a deploy that sets
// no OTEL_* variable gets exactly the handlers it had before.
func TestSetupLoggerOTLPIsOptIn(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		disabled string
		want     bool
	}{
		{name: "no endpoint", want: false},
		{name: "endpoint set", endpoint: "http://127.0.0.1:1", want: true},
		{name: "endpoint set but sdk disabled", endpoint: "http://127.0.0.1:1", disabled: "true", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", tt.endpoint)
			t.Setenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", "")
			t.Setenv("OTEL_SDK_DISABLED", tt.disabled)

			logger, shipper, otelLogs := setupLogger(config.Config{LogLevel: slog.LevelInfo})
			if logger == nil {
				t.Fatal("logger is nil")
			}
			if shipper != nil {
				t.Error("shipper started with log shipping disabled")
			}
			if got := otelLogs != nil; got != tt.want {
				t.Fatalf("otel logs enabled = %v, want %v", got, tt.want)
			}
			if otelLogs != nil {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				if err := otelLogs.Shutdown(ctx); err != nil {
					t.Errorf("Shutdown with nothing buffered: %v", err)
				}
			}
		})
	}
}
