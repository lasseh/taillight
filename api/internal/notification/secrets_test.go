package notification

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestChannelRedacted(t *testing.T) {
	tests := []struct {
		name       string
		typ        ChannelType
		config     string
		wantSecret bool // true if a masked value must be present
		wantKept   []string
	}{
		{
			name:       "slack webhook url masked",
			typ:        ChannelTypeSlack,
			config:     `{"webhook_url":"https://hooks.slack.com/services/T/B/secret"}`,
			wantSecret: true,
		},
		{
			name:       "webhook url and headers masked, method kept",
			typ:        ChannelTypeWebhook,
			config:     `{"url":"https://x/hook?token=abc","method":"POST","headers":{"Authorization":"Bearer s"}}`,
			wantSecret: true,
			wantKept:   []string{"POST", "Authorization"},
		},
		{
			name:       "ntfy token masked, topic kept",
			typ:        ChannelTypeNtfy,
			config:     `{"server_url":"https://ntfy.sh","topic":"alerts","token":"tk_secret"}`,
			wantSecret: true,
			wantKept:   []string{"alerts", "ntfy.sh"},
		},
		{
			name:       "email has no secrets",
			typ:        ChannelTypeEmail,
			config:     `{"to":["ops@example.com"]}`,
			wantSecret: false,
			wantKept:   []string{"ops@example.com"},
		},
		{
			name:       "empty secret not masked",
			typ:        ChannelTypeNtfy,
			config:     `{"topic":"alerts","token":""}`,
			wantSecret: false,
			wantKept:   []string{"alerts"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := Channel{Type: tt.typ, Config: json.RawMessage(tt.config)}.Redacted()
			got := string(out.Config)

			hasMask := bytes.Contains(out.Config, []byte(RedactedSecret))
			if hasMask != tt.wantSecret {
				t.Errorf("masked=%v, want %v; config=%s", hasMask, tt.wantSecret, got)
			}
			// The original secret values must never appear in the output.
			for _, leaked := range []string{"secret", "token=abc", "Bearer s", "tk_secret"} {
				if bytes.Contains(out.Config, []byte(leaked)) {
					t.Errorf("secret %q leaked in %s", leaked, got)
				}
			}
			for _, keep := range tt.wantKept {
				if !bytes.Contains(out.Config, []byte(keep)) {
					t.Errorf("expected %q kept in %s", keep, got)
				}
			}
		})
	}
}

// TestChannelWithSecretsFrom covers the edit round trip: a read response sent
// back unchanged must reproduce the stored config, and re-typed secrets win.
func TestChannelWithSecretsFrom(t *testing.T) {
	tests := []struct {
		name   string
		typ    ChannelType
		stored string
		edit   func(redacted map[string]any) // nil = send the redacted read back as-is
		want   string
	}{
		{
			name:   "ntfy token survives an unchanged edit",
			typ:    ChannelTypeNtfy,
			stored: `{"server_url":"https://ntfy.sh","token":"tk_secret","topic":"alerts"}`,
			want:   `{"server_url":"https://ntfy.sh","token":"tk_secret","topic":"alerts"}`,
		},
		{
			name:   "slack url survives an unchanged edit",
			typ:    ChannelTypeSlack,
			stored: `{"webhook_url":"https://hooks.slack.com/services/T/B/secret"}`,
			want:   `{"webhook_url":"https://hooks.slack.com/services/T/B/secret"}`,
		},
		{
			name:   "webhook headers restored per member, new header kept",
			typ:    ChannelTypeWebhook,
			stored: `{"headers":{"Authorization":"Bearer s"},"url":"https://x/hook?token=abc"}`,
			edit: func(c map[string]any) {
				c["headers"].(map[string]any)["X-New"] = "v"
			},
			want: `{"headers":{"Authorization":"Bearer s","X-New":"v"},"url":"https://x/hook?token=abc"}`,
		},
		{
			name:   "re-typed secret replaces stored",
			typ:    ChannelTypeNtfy,
			stored: `{"token":"old","topic":"alerts"}`,
			edit:   func(c map[string]any) { c["token"] = "new" },
			want:   `{"token":"new","topic":"alerts"}`,
		},
		{
			name:   "removed secret stays removed",
			typ:    ChannelTypeNtfy,
			stored: `{"token":"old","topic":"alerts"}`,
			edit:   func(c map[string]any) { delete(c, "token") },
			want:   `{"topic":"alerts"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stored := Channel{Type: tt.typ, Config: json.RawMessage(tt.stored)}
			var cfg map[string]any
			if err := json.Unmarshal(stored.Redacted().Config, &cfg); err != nil {
				t.Fatal(err)
			}
			if tt.edit != nil {
				tt.edit(cfg)
			}
			sent, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}

			got := Channel{Type: tt.typ, Config: sent}.WithSecretsFrom(stored)
			if string(got.Config) != tt.want {
				t.Errorf("got %s, want %s", got.Config, tt.want)
			}
		})
	}
}

func TestChannelHasRedactedSecret(t *testing.T) {
	tests := []struct {
		name   string
		typ    ChannelType
		config string
		want   bool
	}{
		{"real token", ChannelTypeNtfy, `{"token":"tk"}`, false},
		{"masked token", ChannelTypeNtfy, `{"token":"********"}`, true},
		{"masked header value", ChannelTypeWebhook, `{"url":"https://x","headers":{"X-Renamed":"********"}}`, true},
		{"mask in a non-secret field", ChannelTypeNtfy, `{"topic":"********"}`, false},
		{"email has no secrets", ChannelTypeEmail, `{"to":["********"]}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := (Channel{Type: tt.typ, Config: json.RawMessage(tt.config)}).HasRedactedSecret(); got != tt.want {
				t.Errorf("HasRedactedSecret = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestChannelWithSecretsFromTypeChange(t *testing.T) {
	stored := Channel{Type: ChannelTypeSlack, Config: json.RawMessage(`{"webhook_url":"https://hooks.slack.com/x"}`)}
	sent := Channel{Type: ChannelTypeWebhook, Config: json.RawMessage(`{"url":"********"}`)}
	if got := sent.WithSecretsFrom(stored); string(got.Config) != `{"url":"********"}` {
		t.Errorf("secret carried across a type change: %s", got.Config)
	}
}
