package main

import (
	"log/slog"
	"testing"
	"time"

	"github.com/lasseh/taillight/pkg/logshipper"
)

func TestParseLine_JSON(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantLevel slog.Level
		wantMsg   string
	}{
		{
			name:      "json with all fields",
			input:     `{"time":"2024-01-15T10:30:00Z","level":"ERROR","msg":"something broke"}`,
			wantLevel: slog.LevelError,
			wantMsg:   "something broke",
		},
		{
			name:      "json with message key",
			input:     `{"level":"WARN","message":"disk full"}`,
			wantLevel: slog.LevelWarn,
			wantMsg:   "disk full",
		},
		{
			name:      "json missing level defaults to INFO",
			input:     `{"msg":"no level here"}`,
			wantLevel: slog.LevelInfo,
			wantMsg:   "no level here",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := parseLine(tt.input)
			if r.Level != tt.wantLevel {
				t.Errorf("level = %v, want %v", r.Level, tt.wantLevel)
			}
			if r.Message != tt.wantMsg {
				t.Errorf("message = %q, want %q", r.Message, tt.wantMsg)
			}
		})
	}
}

func TestParseLine_PlainText(t *testing.T) {
	r := parseLine("just a plain log line")
	if r.Level != slog.LevelInfo {
		t.Errorf("level = %v, want INFO", r.Level)
	}
	if r.Message != "just a plain log line" {
		t.Errorf("message = %q, want %q", r.Message, "just a plain log line")
	}
}

func TestExtractTime(t *testing.T) {
	tests := []struct {
		name     string
		m        map[string]any
		wantTime time.Time
		wantNow  bool // true = expect time.Now() (approximately)
	}{
		{
			name:     "RFC3339",
			m:        map[string]any{timeKey: "2024-01-15T10:30:00Z"},
			wantTime: time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		},
		{
			name:     "RFC3339Nano",
			m:        map[string]any{timeKey: "2024-01-15T10:30:00.123456789Z"},
			wantTime: time.Date(2024, 1, 15, 10, 30, 0, 123456789, time.UTC),
		},
		{
			name:     "timestamp key",
			m:        map[string]any{"timestamp": "2024-06-01T12:00:00Z"},
			wantTime: time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC),
		},
		{
			name:    "missing field",
			m:       map[string]any{"msg": "hello"},
			wantNow: true,
		},
		{
			name:    "non-string value",
			m:       map[string]any{timeKey: 12345},
			wantNow: true,
		},
		{
			name:    "bad format",
			m:       map[string]any{timeKey: "not-a-time"},
			wantNow: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := time.Now()
			got := extractTime(tt.m)
			after := time.Now()

			if tt.wantNow {
				if got.Before(before) || got.After(after) {
					t.Errorf("expected time.Now(), got %v", got)
				}
			} else if !got.Equal(tt.wantTime) {
				t.Errorf("got %v, want %v", got, tt.wantTime)
			}
		})
	}
}

func TestExtractTime_DeletesKey(t *testing.T) {
	m := map[string]any{timeKey: "2024-01-15T10:30:00Z", "msg": "hello"}
	extractTime(m)
	if _, ok := m[timeKey]; ok {
		t.Error("expected time key to be deleted")
	}
	if _, ok := m["msg"]; !ok {
		t.Error("expected msg key to remain")
	}
}

func TestExtractLevel(t *testing.T) {
	tests := []struct {
		name string
		m    map[string]any
		want slog.Level
	}{
		{name: "DEBUG", m: map[string]any{"level": "DEBUG"}, want: slog.LevelDebug},
		{name: "debug lowercase", m: map[string]any{"level": "debug"}, want: slog.LevelDebug},
		{name: "INFO", m: map[string]any{"level": "INFO"}, want: slog.LevelInfo},
		{name: "WARN", m: map[string]any{"level": "WARN"}, want: slog.LevelWarn},
		{name: "WARNING", m: map[string]any{"level": "WARNING"}, want: slog.LevelWarn},
		{name: "ERROR", m: map[string]any{"level": "ERROR"}, want: slog.LevelError},
		{name: "TRACE", m: map[string]any{"level": "TRACE"}, want: slog.LevelDebug},
		{name: "FATAL", m: map[string]any{"level": "FATAL"}, want: logshipper.LevelFatal},
		{name: "CRITICAL", m: map[string]any{"level": "CRITICAL"}, want: logshipper.LevelFatal},
		{name: "PANIC", m: map[string]any{"level": "PANIC"}, want: logshipper.LevelFatal},
		{name: "unknown defaults to INFO", m: map[string]any{"level": "VERBOSE"}, want: slog.LevelInfo},
		{name: "missing defaults to INFO", m: map[string]any{}, want: slog.LevelInfo},
		{name: "non-string defaults to INFO", m: map[string]any{"level": 42}, want: slog.LevelInfo},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractLevel(tt.m)
			if got != tt.want {
				t.Errorf("extractLevel() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractString(t *testing.T) {
	tests := []struct {
		name string
		m    map[string]any
		keys []string
		want string
	}{
		{
			name: "first key found",
			m:    map[string]any{"msg": "hello", "message": "world"},
			keys: []string{"msg", "message"},
			want: "hello",
		},
		{
			name: "second key found",
			m:    map[string]any{"message": "world"},
			keys: []string{"msg", "message"},
			want: "world",
		},
		{
			name: "missing returns empty",
			m:    map[string]any{"foo": "bar"},
			keys: []string{"msg", "message"},
			want: "",
		},
		{
			name: "non-string value returns empty",
			m:    map[string]any{"msg": 42},
			keys: []string{"msg"},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractString(tt.m, tt.keys...)
			if got != tt.want {
				t.Errorf("extractString() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractString_DeletesKey(t *testing.T) {
	m := map[string]any{"msg": "hello", "extra": "keep"}
	extractString(m, "msg")
	if _, ok := m["msg"]; ok {
		t.Error("expected msg key to be deleted")
	}
	if _, ok := m["extra"]; !ok {
		t.Error("expected extra key to remain")
	}
}

func TestPlainRecord(t *testing.T) {
	before := time.Now()
	r := plainRecord("a log line")
	after := time.Now()

	if r.Level != slog.LevelInfo {
		t.Errorf("level = %v, want INFO", r.Level)
	}
	if r.Message != "a log line" {
		t.Errorf("message = %q, want %q", r.Message, "a log line")
	}
	if r.Time.Before(before) || r.Time.After(after) {
		t.Errorf("expected time near now, got %v", r.Time)
	}
}
