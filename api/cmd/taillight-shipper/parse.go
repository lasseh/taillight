package main

import (
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/lasseh/taillight/pkg/logshipper"
)

// parseLine attempts to parse a line as JSON. If that fails, it treats the line
// as plain text with INFO level and the current time.
func parseLine(line string) slog.Record {
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		return plainRecord(line)
	}
	return jsonRecord(m)
}

func plainRecord(line string) slog.Record {
	r := slog.NewRecord(time.Now(), slog.LevelInfo, line, 0)
	return r
}

func jsonRecord(m map[string]any) slog.Record {
	ts := extractTime(m)
	level := extractLevel(m)
	msg := extractString(m, "msg", "message")

	r := slog.NewRecord(ts, level, msg, 0)

	for k, v := range m {
		r.AddAttrs(slog.Any(k, v))
	}

	return r
}

func extractTime(m map[string]any) time.Time {
	for _, key := range []string{"time", "timestamp"} {
		v, ok := m[key]
		if !ok {
			continue
		}
		delete(m, key)

		s, ok := v.(string)
		if !ok {
			continue
		}

		t, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			t, err = time.Parse(time.RFC3339, s)
		}
		if err == nil {
			return t
		}
	}
	return time.Now()
}

func extractLevel(m map[string]any) slog.Level {
	v, ok := m["level"]
	if !ok {
		return slog.LevelInfo
	}
	delete(m, "level")

	s, ok := v.(string)
	if !ok {
		return slog.LevelInfo
	}

	switch strings.ToUpper(s) {
	case "TRACE", "DEBUG":
		return slog.LevelDebug
	case "INFO":
		return slog.LevelInfo
	case "WARN", "WARNING":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	case "FATAL", "CRITICAL", "PANIC":
		return logshipper.LevelFatal
	default:
		return slog.LevelInfo
	}
}

func extractString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		v, ok := m[key]
		if !ok {
			continue
		}
		delete(m, key)

		s, ok := v.(string)
		if ok {
			return s
		}
	}
	return ""
}
