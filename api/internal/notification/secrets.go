package notification

import (
	"encoding/json"
	"net/url"
	"reflect"
	"slices"
	"strings"
)

// RedactedSecret is the placeholder substituted for a configured secret value
// so a client can tell a secret is set without learning its value.
const RedactedSecret = "********"

// secretKeys lists the config JSON keys, per channel type, that carry delivery
// credentials. The table lives here rather than on the backends so redaction
// still works when the engine (and its backends) is disabled. Config keys
// match it case-insensitively, the way encoding/json matches them to the
// backends' config fields. The webhook template is secret because it is the
// request body and can carry a credential. The ntfy topic is secret because
// anyone who knows a topic on a shared server can read it.
var secretKeys = map[ChannelType][]string{
	ChannelTypeSlack:   {"webhook_url"},
	ChannelTypeWebhook: {"url", "headers", "template"},
	ChannelTypeNtfy:    {"token", "topic"},
}

// destinationKeys names the config JSON key, per channel type, that sets
// where a notification is delivered. A destination that is not in secretKeys
// is masked when it may carry URL userinfo, and a masked one is restored on
// edit like a secret.
var destinationKeys = map[ChannelType]string{
	ChannelTypeSlack:   "webhook_url",
	ChannelTypeWebhook: "url",
	ChannelTypeNtfy:    "server_url",
}

// SecretKeys returns the config JSON keys that hold credentials for t.
func SecretKeys(t ChannelType) []string {
	return slices.Clone(secretKeys[t])
}

// Redacted returns a copy of ch with every configured secret replaced by
// RedactedSecret. A JSON object secret (webhook headers) keeps its key names
// and masks each value, so the response shape stays stable for the frontend.
// Keys keep their spelling, so a secret under a case variant is masked in
// place. Malformed config fails closed to `{}`.
func (ch Channel) Redacted() Channel {
	if len(secretKeys[ch.Type]) == 0 || len(ch.Config) == 0 {
		return ch
	}
	var cfg map[string]json.RawMessage
	if err := json.Unmarshal(ch.Config, &cfg); err != nil {
		ch.Config = json.RawMessage(`{}`)
		return ch
	}
	for k, raw := range cfg {
		if isEmptyJSON(raw) {
			continue
		}
		if isSecretKey(ch.Type, k) || (isDestinationKey(ch.Type, k) && mayHaveUserinfo(raw)) {
			cfg[k] = maskJSONValue(raw)
		}
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
// member. A placeholder is restored only from the stored member of the same
// name; one with no stored counterpart is left as-is so HasRedactedSecret
// rejects it. A stored secret is kept only for the destination it was saved
// for: if the merged config delivers anywhere else, ch is returned unrestored
// and its placeholders are rejected the same way.
func (ch Channel) WithSecretsFrom(stored Channel) Channel {
	if len(secretKeys[ch.Type]) == 0 || ch.Type != stored.Type || len(ch.Config) == 0 {
		return ch
	}
	var cfg, old map[string]json.RawMessage
	if json.Unmarshal(ch.Config, &cfg) != nil || json.Unmarshal(stored.Config, &old) != nil {
		return ch
	}
	for k, raw := range cfg {
		if isRestorableKey(ch.Type, k) {
			cfg[k] = restoreJSONValue(raw, old[k])
		}
	}
	if !sameDestination(ch.Type, cfg, old) {
		return ch
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
	for k, raw := range cfg {
		if !isRestorableKey(ch.Type, k) {
			continue
		}
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

// isSecretKey reports whether config key k of a t channel holds a secret.
func isSecretKey(t ChannelType, k string) bool {
	return slices.ContainsFunc(secretKeys[t], func(s string) bool { return strings.EqualFold(s, k) })
}

// isDestinationKey reports whether config key k of a t channel sets where it
// delivers.
func isDestinationKey(t ChannelType, k string) bool {
	d, ok := destinationKeys[t]
	return ok && strings.EqualFold(d, k)
}

// isRestorableKey reports whether a placeholder in config key k of a t channel
// stands for a stored value: a secret, or a destination masked for userinfo.
func isRestorableKey(t ChannelType, k string) bool {
	return isSecretKey(t, k) || isDestinationKey(t, k)
}

// sameDestination reports whether configs a and b of a t channel deliver to
// the same place. Every member whose key matches the destination key counts,
// by exact name and decoded value, so the answer does not depend on which of
// several case variants the backend's decoder ends up using.
func sameDestination(t ChannelType, a, b map[string]json.RawMessage) bool {
	da, okA := destinationMembers(t, a)
	db, okB := destinationMembers(t, b)
	return okA && okB && reflect.DeepEqual(da, db)
}

// destinationMembers returns the decoded destination members of cfg.
func destinationMembers(t ChannelType, cfg map[string]json.RawMessage) (map[string]any, bool) {
	out := make(map[string]any)
	for k, raw := range cfg {
		if !isDestinationKey(t, k) {
			continue
		}
		var v any
		if json.Unmarshal(raw, &v) != nil {
			return nil, false
		}
		out[k] = v
	}
	return out, true
}

// mayHaveUserinfo reports whether a destination value may carry URL
// credentials. A value that is not a string URL it can parse counts as
// carrying them, so redaction fails closed.
func mayHaveUserinfo(raw json.RawMessage) bool {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return true
	}
	u, err := url.Parse(s)
	return err != nil || u.User != nil
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
