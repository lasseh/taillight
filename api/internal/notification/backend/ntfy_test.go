package backend

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/lasseh/taillight/internal/model"
	"github.com/lasseh/taillight/internal/notification"
)

func TestNtfyValidate(t *testing.T) {
	n := NewNtfy(discardLogger())

	tests := []struct {
		name    string
		config  ntfyConfig
		wantErr string
	}{
		{
			name:    "missing server_url",
			config:  ntfyConfig{Topic: "alerts"},
			wantErr: "server_url is required",
		},
		{
			name:    "invalid scheme",
			config:  ntfyConfig{ServerURL: "ftp://ntfy.example.com", Topic: "alerts"},
			wantErr: "server_url must use HTTP or HTTPS",
		},
		{
			name:    "missing topic",
			config:  ntfyConfig{ServerURL: "https://ntfy.example.com"},
			wantErr: "topic is required",
		},
		{
			name:    "priority too high",
			config:  ntfyConfig{ServerURL: "https://ntfy.example.com", Topic: "alerts", Priority: 6},
			wantErr: "priority must be between 0 and 5",
		},
		{
			name:    "negative priority",
			config:  ntfyConfig{ServerURL: "https://ntfy.example.com", Topic: "alerts", Priority: -1},
			wantErr: "priority must be between 0 and 5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, _ := json.Marshal(tt.config)
			ch := notification.Channel{Config: raw}
			err := n.Validate(ch)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}

func TestNtfyValidateInvalidJSON(t *testing.T) {
	n := NewNtfy(discardLogger())
	ch := notification.Channel{Config: json.RawMessage(`{invalid`)}
	err := n.Validate(ch)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "invalid ntfy config") {
		t.Fatalf("expected 'invalid ntfy config' error, got %q", err.Error())
	}
}

func TestBuildNtfyInitialSrvlog(t *testing.T) {
	p := notification.Payload{
		Kind: notification.EventKindSrvlog,
		SrvlogEvent: &model.SrvlogEvent{
			Hostname:    "router1",
			Programname: "rpd",
			Severity:    2,
			Message:     "bgp peer down",
		},
		EventCount: 1,
	}

	title, body := buildNtfyMessage(p)

	if title != "[CRIT] router1 rpd" {
		t.Errorf("expected title %q, got %q", "[CRIT] router1 rpd", title)
	}
	if body != "bgp peer down" {
		t.Errorf("expected body %q, got %q", "bgp peer down", body)
	}
}

func TestBuildNtfyInitialMultipleEvents(t *testing.T) {
	p := notification.Payload{
		Kind: notification.EventKindSrvlog,
		SrvlogEvent: &model.SrvlogEvent{
			Hostname:    "web01",
			Programname: "nginx",
			Severity:    3,
			Message:     "upstream timeout",
		},
		EventCount: 5,
	}

	title, _ := buildNtfyMessage(p)

	if title != "[ERR] web01 nginx (5 events)" {
		t.Errorf("expected title %q, got %q", "[ERR] web01 nginx (5 events)", title)
	}
}

func TestBuildNtfyInitialApplog(t *testing.T) {
	p := notification.Payload{
		Kind: notification.EventKindAppLog,
		AppLogEvent: &model.AppLogEvent{
			Service: "my-api",
			Host:    "prod-1",
			Level:   "ERROR",
			Msg:     "connection refused",
		},
		EventCount: 3,
	}

	title, body := buildNtfyMessage(p)

	if title != "[ERROR] my-api prod-1 (3 events)" {
		t.Errorf("expected title %q, got %q", "[ERROR] my-api prod-1 (3 events)", title)
	}
	if body != "connection refused" {
		t.Errorf("expected body %q, got %q", "connection refused", body)
	}
}

func TestBuildNtfyDigestSrvlog(t *testing.T) {
	p := notification.Payload{
		Kind:     notification.EventKindSrvlog,
		IsDigest: true,
		SrvlogEvent: &model.SrvlogEvent{
			Hostname: "web01",
			Severity: 3,
			Message:  "last error message",
		},
		EventCount: 42,
		Window:     5 * time.Minute,
	}

	title, body := buildNtfyMessage(p)

	if title != "[ERR] web01 (digest)" {
		t.Errorf("expected title %q, got %q", "[ERR] web01 (digest)", title)
	}
	if !strings.Contains(body, "42 more events") {
		t.Errorf("expected body to contain '42 more events', got %q", body)
	}
	if !strings.Contains(body, "5 minutes") {
		t.Errorf("expected body to contain '5 minutes', got %q", body)
	}
}

func TestBuildNtfyDigestApplog(t *testing.T) {
	p := notification.Payload{
		Kind:     notification.EventKindAppLog,
		IsDigest: true,
		AppLogEvent: &model.AppLogEvent{
			Service: "my-api",
			Level:   "FATAL",
			Msg:     "out of memory",
		},
		EventCount: 10,
		Window:     30 * time.Second,
	}

	title, body := buildNtfyMessage(p)

	if title != "[FATAL] my-api (digest)" {
		t.Errorf("expected title %q, got %q", "[FATAL] my-api (digest)", title)
	}
	if !strings.Contains(body, "30 seconds") {
		t.Errorf("expected body to contain '30 seconds', got %q", body)
	}
}

func TestNtfyPriority(t *testing.T) {
	tests := []struct {
		name     string
		payload  notification.Payload
		expected int
	}{
		{"srvlog emerg", notification.Payload{SrvlogEvent: &model.SrvlogEvent{Severity: 0}}, 5},
		{"srvlog alert", notification.Payload{SrvlogEvent: &model.SrvlogEvent{Severity: 1}}, 5},
		{"srvlog crit", notification.Payload{SrvlogEvent: &model.SrvlogEvent{Severity: 2}}, 4},
		{"srvlog error", notification.Payload{SrvlogEvent: &model.SrvlogEvent{Severity: 3}}, 4},
		{"srvlog warning", notification.Payload{SrvlogEvent: &model.SrvlogEvent{Severity: 4}}, 3},
		{"srvlog notice", notification.Payload{SrvlogEvent: &model.SrvlogEvent{Severity: 5}}, 2},
		{"srvlog info", notification.Payload{SrvlogEvent: &model.SrvlogEvent{Severity: 6}}, 2},
		{"srvlog debug", notification.Payload{SrvlogEvent: &model.SrvlogEvent{Severity: 7}}, 2},
		{"applog fatal", notification.Payload{AppLogEvent: &model.AppLogEvent{Level: "FATAL"}}, 5},
		{"applog error", notification.Payload{AppLogEvent: &model.AppLogEvent{Level: "ERROR"}}, 4},
		{"applog warn", notification.Payload{AppLogEvent: &model.AppLogEvent{Level: "WARN"}}, 3},
		{"applog info", notification.Payload{AppLogEvent: &model.AppLogEvent{Level: "INFO"}}, 2},
		{"applog debug", notification.Payload{AppLogEvent: &model.AppLogEvent{Level: "DEBUG"}}, 2},
		{"no event", notification.Payload{}, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ntfyPriority(tt.payload)
			if got != tt.expected {
				t.Errorf("expected priority %d, got %d", tt.expected, got)
			}
		})
	}
}

func TestNtfyTags(t *testing.T) {
	tests := []struct {
		name     string
		payload  notification.Payload
		expected string
	}{
		{"srvlog emerg", notification.Payload{SrvlogEvent: &model.SrvlogEvent{Severity: 0}}, "rotating_light"},
		{"srvlog crit", notification.Payload{SrvlogEvent: &model.SrvlogEvent{Severity: 2}}, "rotating_light"},
		{"srvlog error", notification.Payload{SrvlogEvent: &model.SrvlogEvent{Severity: 3}}, "x"},
		{"srvlog warning", notification.Payload{SrvlogEvent: &model.SrvlogEvent{Severity: 4}}, "warning"},
		{"srvlog info", notification.Payload{SrvlogEvent: &model.SrvlogEvent{Severity: 6}}, "information_source"},
		{"srvlog debug", notification.Payload{SrvlogEvent: &model.SrvlogEvent{Severity: 7}}, "mag"},
		{"applog fatal", notification.Payload{AppLogEvent: &model.AppLogEvent{Level: "FATAL"}}, "rotating_light"},
		{"applog error", notification.Payload{AppLogEvent: &model.AppLogEvent{Level: "ERROR"}}, "x"},
		{"applog warn", notification.Payload{AppLogEvent: &model.AppLogEvent{Level: "WARN"}}, "warning"},
		{"applog info", notification.Payload{AppLogEvent: &model.AppLogEvent{Level: "INFO"}}, "information_source"},
		{"no event", notification.Payload{}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ntfyTags(tt.payload)
			if got != tt.expected {
				t.Errorf("expected tags %q, got %q", tt.expected, got)
			}
		})
	}
}
