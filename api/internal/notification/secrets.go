package notification

import (
	"encoding/json"
	"slices"
)

// RedactedSecret is the placeholder substituted for a configured secret value
// so a client can tell a secret is set without learning its value.
const RedactedSecret = "********"

// secretKeys lists the config JSON keys, per channel type, that carry delivery
// credentials. The table lives here rather than on the backends so redaction
// still works when the engine (and its backends) is disabled.
var secretKeys = map[ChannelType][]string{
	ChannelTypeSlack:   {"webhook_url"},
	ChannelTypeWebhook: {"url", "headers"},
	ChannelTypeNtfy:    {"token"},
}

// SecretKeys returns the config JSON keys that hold credentials for t.
func SecretKeys(t ChannelType) []string {
	return slices.Clone(secretKeys[t])
}

// Redacted returns a copy of ch with every configured secret replaced by
// RedactedSecret. A JSON object secret (webhook headers) keeps its key names
// and masks each value, so the response shape stays stable for the frontend.
// Malformed config fails closed to `{}`.
func (ch Channel) Redacted() Channel {
	keys := secretKeys[ch.Type]
	if len(keys) == 0 || len(ch.Config) == 0 {
		return ch
	}
	var cfg map[string]json.RawMessage
	if err := json.Unmarshal(ch.Config, &cfg); err != nil {
		ch.Config = json.RawMessage(`{}`)
		return ch
	}
	for _, k := range keys {
		raw, present := cfg[k]
		if !present || isEmptyJSON(raw) {
			continue
		}
		cfg[k] = maskJSONValue(raw)
	}
	redacted, err := json.Marshal(cfg)
	if err != nil {
		ch.Config = json.RawMessage(`{}`)
		return ch
	}
	ch.Config = redacted
	return ch
}

// WithSecretsFrom returns ch with every RedactedSecret placeholder replaced by
// the matching value in stored. It reverses Redacted for an edit that sends a
// read response back unchanged: a masked secret keeps its stored value, while
// a secret the client re-typed replaces it. Header objects are merged per
// member. A placeholder with no stored counterpart is left as-is so backend
// validation can reject it.
func (ch Channel) WithSecretsFrom(stored Channel) Channel {
	keys := secretKeys[ch.Type]
	if len(keys) == 0 || ch.Type != stored.Type || len(ch.Config) == 0 {
		return ch
	}
	var cfg, old map[string]json.RawMessage
	if json.Unmarshal(ch.Config, &cfg) != nil || json.Unmarshal(stored.Config, &old) != nil {
		return ch
	}
	for _, k := range keys {
		raw, present := cfg[k]
		if !present {
			continue
		}
		cfg[k] = restoreJSONValue(raw, old[k])
	}
	merged, err := json.Marshal(cfg)
	if err != nil {
		return ch
	}
	ch.Config = merged
	return ch
}

// HasRedactedSecret reports whether any secret in ch is still the
// placeholder, e.g. a renamed webhook header whose value was never re-typed.
// Such a config must not be stored: the placeholder would become the secret.
func (ch Channel) HasRedactedSecret() bool {
	var cfg map[string]json.RawMessage
	if json.Unmarshal(ch.Config, &cfg) != nil {
		return false
	}
	for _, k := range secretKeys[ch.Type] {
		raw := cfg[k]
		if isMasked(raw) {
			return true
		}
		var obj map[string]json.RawMessage
		if len(raw) > 0 && raw[0] == '{' && json.Unmarshal(raw, &obj) == nil {
			for _, v := range obj {
				if isMasked(v) {
					return true
				}
			}
		}
	}
	return false
}

// maskJSONValue replaces a secret JSON value with the placeholder. For a JSON
// object it masks each member value and keeps the member names.
func maskJSONValue(raw json.RawMessage) json.RawMessage {
	masked, _ := json.Marshal(RedactedSecret)
	if len(raw) == 0 || raw[0] != '{' {
		return masked
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return masked
	}
	for k := range obj {
		obj[k] = masked
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return masked
	}
	return out
}

// restoreJSONValue is the inverse of maskJSONValue against the stored value.
func restoreJSONValue(raw, stored json.RawMessage) json.RawMessage {
	if isMasked(raw) {
		if stored == nil {
			return raw
		}
		return stored
	}
	if len(raw) == 0 || raw[0] != '{' {
		return raw
	}
	var obj, oldObj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil || json.Unmarshal(stored, &oldObj) != nil {
		return raw
	}
	for k, v := range obj {
		if old, ok := oldObj[k]; ok && isMasked(v) {
			obj[k] = old
		}
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return raw
	}
	return out
}

func isMasked(raw json.RawMessage) bool {
	var s string
	return json.Unmarshal(raw, &s) == nil && s == RedactedSecret
}

// isEmptyJSON reports whether a raw JSON value is null, "", {}, or [].
func isEmptyJSON(raw json.RawMessage) bool {
	switch string(raw) {
	case "", "null", `""`, "{}", "[]":
		return true
	default:
		return false
	}
}
