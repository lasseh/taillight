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
			name:       "ntfy token and topic masked, server kept",
			typ:        ChannelTypeNtfy,
			config:     `{"server_url":"https://ntfy.sh","topic":"alerts","token":"tk_secret"}`,
			wantSecret: true,
			wantKept:   []string{"ntfy.sh"},
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
			config:     `{"server_url":"https://ntfy.sh","token":""}`,
			wantSecret: false,
			wantKept:   []string{"ntfy.sh"},
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
			for _, leaked := range []string{"secret", "token=abc", "Bearer s", "tk_secret", "alerts"} {
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

// TestChannelRedactedCredentialFields covers credentials outside the canonical
// secret keys: userinfo in the ntfy server URL, the webhook template, and keys
// spelled in another case, which the backends' JSON decoding still uses.
func TestChannelRedactedCredentialFields(t *testing.T) {
	tests := []struct {
		name    string
		typ     ChannelType
		config  string
		secrets []string // must not appear in the output
		kept    []string // must appear in the output
	}{
		{
			name:    "ntfy userinfo in server_url and topic",
			typ:     ChannelTypeNtfy,
			config:  `{"server_url":"http://ntfyuser:dummy-ntfy-password@198.51.100.20","topic":"dummy-secret-topic"}`,
			secrets: []string{"ntfyuser", "dummy-ntfy-password", "dummy-secret-topic", "198.51.100.20"},
			kept:    []string{`"server_url"`, `"topic"`},
		},
		{
			name:    "webhook template",
			typ:     ChannelTypeWebhook,
			config:  `{"url":"http://198.51.100.20/v2/enqueue","template":"{\"routing_key\":\"dummy-routing-key\",\"event_action\":\"trigger\"}"}`,
			secrets: []string{"dummy-routing-key", "enqueue"},
			kept:    []string{`"template"`, `"url"`},
		},
		{
			name:    "webhook url and headers in another case",
			typ:     ChannelTypeWebhook,
			config:  `{"URL":"http://198.51.100.20/hook/PATHSECRET","Headers":{"X-Api-Key":"dummy-header-secret"}}`,
			secrets: []string{"PATHSECRET", "dummy-header-secret"},
			kept:    []string{`"URL"`, `"Headers"`, `"X-Api-Key"`},
		},
		{
			name:    "ntfy token in another case",
			typ:     ChannelTypeNtfy,
			config:  `{"server_url":"http://198.51.100.20","topic":"t","Token":"dummy-ntfy-token"}`,
			secrets: []string{"dummy-ntfy-token"},
			kept:    []string{`"Token"`, "http://198.51.100.20"},
		},
		{
			name:    "ntfy server_url key that folds to s",
			typ:     ChannelTypeNtfy,
			config:  `{"ſERVER_URL":"http://u:dummy-folded-password@198.51.100.20","topic":"t"}`,
			secrets: []string{"dummy-folded-password"},
		},
		{
			name:    "ntfy server_url that does not parse",
			typ:     ChannelTypeNtfy,
			config:  `{"server_url":"http://u:dummy-unparsed-password@198.51.100.20/\u007f","topic":"t"}`,
			secrets: []string{"dummy-unparsed-password"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := Channel{Type: tt.typ, Config: json.RawMessage(tt.config)}.Redacted()
			if !bytes.Contains(out.Config, []byte(RedactedSecret)) {
				t.Errorf("nothing masked: %s", out.Config)
			}
			for _, s := range tt.secrets {
				if bytes.Contains(out.Config, []byte(s)) {
					t.Errorf("%q leaked in %s", s, out.Config)
				}
			}
			for _, s := range tt.kept {
				if !bytes.Contains(out.Config, []byte(s)) {
					t.Errorf("expected %q kept in %s", s, out.Config)
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
		{
			name:   "token in another case survives an unchanged edit",
			typ:    ChannelTypeNtfy,
			stored: `{"Token":"tk_secret","server_url":"https://ntfy.sh","topic":"alerts"}`,
			want:   `{"Token":"tk_secret","server_url":"https://ntfy.sh","topic":"alerts"}`,
		},
		{
			name:   "webhook template survives an unchanged edit",
			typ:    ChannelTypeWebhook,
			stored: `{"template":"{\"key\":\"body-secret\"}","url":"https://x/hook"}`,
			want:   `{"template":"{\"key\":\"body-secret\"}","url":"https://x/hook"}`,
		},
		{
			name:   "ntfy server_url with userinfo survives an unchanged edit",
			typ:    ChannelTypeNtfy,
			stored: `{"server_url":"https://u:pw@ntfy.sh","topic":"alerts"}`,
			want:   `{"server_url":"https://u:pw@ntfy.sh","topic":"alerts"}`,
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

// TestChannelWithSecretsFromNewDestination checks that a masked secret is kept
// only while the merged config delivers where the stored one does. Otherwise
// the placeholder stays, so HasRedactedSecret refuses the edit.
func TestChannelWithSecretsFromNewDestination(t *testing.T) {
	const ntfyStored = `{"server_url":"https://ntfy.sh","token":"tk_secret","topic":"alerts"}`
	const hookStored = `{"headers":{"X-Key":"hk_secret"},"template":"{\"k\":\"tpl_secret\"}","url":"https://x/hook"}`
	tests := []struct {
		name       string
		typ        ChannelType
		stored     string
		sent       string
		wantMasked bool
	}{
		{"ntfy new server", ChannelTypeNtfy, ntfyStored, `{"server_url":"https://other.example","token":"********","topic":"********"}`, true},
		{"ntfy new server under a folded key", ChannelTypeNtfy, ntfyStored, "{\"ſerver_url\":\"https://other.example\",\"server_url\":\"https://ntfy.sh\",\"token\":\"********\",\"topic\":\"********\"}", true},
		{"ntfy server removed", ChannelTypeNtfy, ntfyStored, `{"token":"********","topic":"********"}`, true},
		{"webhook new url with masked header", ChannelTypeWebhook, hookStored, `{"url":"https://y/hook","headers":{"X-Key":"********"}}`, true},
		{"webhook new url with masked template", ChannelTypeWebhook, hookStored, `{"url":"https://y/hook","template":"********"}`, true},
		{"webhook second url key", ChannelTypeWebhook, hookStored, `{"url":"********","URL":"https://y/hook","headers":{"X-Key":"********"}}`, true},
		{"ntfy new server with secrets re-typed", ChannelTypeNtfy, ntfyStored, `{"server_url":"https://other.example","token":"new","topic":"other"}`, false},
		{"ntfy same server", ChannelTypeNtfy, ntfyStored, `{"server_url":"https://ntfy.sh","token":"********","topic":"********"}`, false},
		{"ntfy same server escaped differently", ChannelTypeNtfy, ntfyStored, `{"server_url":"https:\/\/ntfy.sh","token":"********","topic":"********"}`, false},
		{"webhook masked url", ChannelTypeWebhook, hookStored, `{"url":"********","headers":{"X-Key":"********"},"template":"********"}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stored := Channel{Type: tt.typ, Config: json.RawMessage(tt.stored)}
			got := Channel{Type: tt.typ, Config: json.RawMessage(tt.sent)}.WithSecretsFrom(stored)
			if masked := got.HasRedactedSecret(); masked != tt.wantMasked {
				t.Errorf("HasRedactedSecret = %v, want %v; config=%s", masked, tt.wantMasked, got.Config)
			}
			if tt.wantMasked && bytes.Contains(got.Config, []byte("_secret")) {
				t.Errorf("stored secret restored toward a new destination: %s", got.Config)
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
		{"mask in a non-secret field", ChannelTypeWebhook, `{"url":"https://x","method":"********"}`, false},
		{"email has no secrets", ChannelTypeEmail, `{"to":["********"]}`, false},
		{"masked token in another case", ChannelTypeNtfy, `{"TOKEN":"********"}`, true},
		{"masked header in another case", ChannelTypeWebhook, `{"url":"https://x","Headers":{"X-Key":"********"}}`, true},
		{"masked template", ChannelTypeWebhook, `{"url":"https://x","template":"********"}`, true},
		{"masked topic", ChannelTypeNtfy, `{"topic":"********"}`, true},
		{"masked server_url", ChannelTypeNtfy, `{"ſerver_url":"********"}`, true},
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
