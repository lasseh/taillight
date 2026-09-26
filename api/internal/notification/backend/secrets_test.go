package backend

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/lasseh/taillight/internal/notification"
)

// TestSecretKeysMatchConfig pins notification.SecretKeys to the backend config
// structs: a renamed JSON tag would otherwise stop redacting that secret.
func TestSecretKeysMatchConfig(t *testing.T) {
	configs := map[notification.ChannelType]any{
		notification.ChannelTypeSlack:   slackConfig{},
		notification.ChannelTypeWebhook: webhookConfig{},
		notification.ChannelTypeNtfy:    ntfyConfig{},
		notification.ChannelTypeEmail:   emailConfig{},
	}
	for typ, cfg := range configs {
		var tags []string
		rt := reflect.TypeOf(cfg)
		for i := range rt.NumField() {
			name, _, _ := strings.Cut(rt.Field(i).Tag.Get("json"), ",")
			tags = append(tags, name)
		}
		for _, k := range notification.SecretKeys(typ) {
			if !slices.Contains(tags, k) {
				t.Errorf("%s: secret key %q is not a field of its config struct", typ, k)
			}
		}
	}
}
