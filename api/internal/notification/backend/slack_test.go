package backend

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/lasseh/taillight/internal/model"
	"github.com/lasseh/taillight/internal/notification"
)

// TestBuildSlackInitial_FitsSectionLimit verifies the section text stays
// within Slack's limit once the message is escaped and the summary and code
// fence are added, and that the cut never splits a character.
func TestBuildSlackInitial_FitsSectionLimit(t *testing.T) {
	tests := []struct {
		name     string
		hostname string
		message  string
	}{
		{"backticks expand when escaped", "router1", strings.Repeat("`", 5000)},
		{"multibyte message", "router1", strings.Repeat("é", 3000)},
		{"multibyte with odd offset", "router1", "x" + strings.Repeat("€", 2000)},
		{"long summary", strings.Repeat("h", 255), strings.Repeat("m", 5000)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := notification.Payload{
				EventCount:  1,
				SrvlogEvent: &model.SrvlogEvent{Hostname: tt.hostname, Severity: 3, Message: tt.message},
			}
			text := buildSlackInitial(p)

			if len(text) > slackTextLimit {
				t.Errorf("text is %d bytes, want at most %d", len(text), slackTextLimit)
			}
			if !utf8.ValidString(text) {
				t.Error("text is not valid UTF-8")
			}
			summary := tt.hostname + " - ERR"
			if !strings.HasPrefix(text, summary+"\n```\n") || !strings.HasSuffix(text, "\n```") {
				t.Fatalf("text lost its summary or fence: %q...%q", text[:min(len(text), 40)], text[max(0, len(text)-10):])
			}
			body := strings.TrimSuffix(strings.TrimPrefix(text, summary+"\n```\n"), "\n```")
			if strings.Contains(body, "```") {
				t.Error("code block body contains a closing fence")
			}
			if !strings.HasSuffix(body, "...") {
				t.Error("truncated body should end with ...")
			}
		})
	}
}

// TestBuildSlackInitial_ShortMessageUnchanged verifies a message that fits is
// sent whole.
func TestBuildSlackInitial_ShortMessageUnchanged(t *testing.T) {
	p := notification.Payload{
		EventCount:  1,
		SrvlogEvent: &model.SrvlogEvent{Hostname: "router1", Severity: 3, Message: "bgp peer down"},
	}
	want := "router1 - ERR\n```\nbgp peer down\n```"
	if got := buildSlackInitial(p); got != want {
		t.Errorf("buildSlackInitial = %q, want %q", got, want)
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		maxLen int
		want   string
	}{
		{"fits", "abc", 3, "abc"},
		{"ascii cut", "abcdef", 3, "abc..."},
		{"backs off a split rune", "aé", 2, "a..."},
		{"keeps a whole rune", "aéb", 3, "aé..."},
		{"zero budget", "abc", 0, "..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := truncate(tt.in, tt.maxLen); got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.in, tt.maxLen, got, tt.want)
			}
		})
	}
}
