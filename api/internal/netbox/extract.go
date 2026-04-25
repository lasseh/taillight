package netbox

import (
	"net/netip"
	"regexp"
	"strconv"
	"strings"

	"github.com/lasseh/jink/lexer"
)

// asnPattern matches ASN references in show-mode log messages: "AS65001",
// "as 65001", "AS  65001". jink only flags TokenASN in config mode, so we
// supplement with this regex for syslog content.
var asnPattern = regexp.MustCompile(`(?i)\bAS\s*(\d{1,10})\b`)

// Extract returns the network entities found in a log message. The hostname
// of the originating device is always included as a "device" entity, and
// any interface tokens carry it as context (interface names aren't unique
// across devices).
//
// Entity values are normalized so cache keys are stable: IPs go through
// netip.ParseAddr; prefixes via netip.ParsePrefix; ASN strips the optional
// "AS" prefix; interface and device names trim whitespace. Duplicate
// entities within one message are deduped.
func Extract(message, hostname string) []Entity {
	hostname = strings.TrimSpace(hostname)
	seen := make(map[string]struct{})
	var out []Entity

	if hostname != "" {
		ent := Entity{Type: EntityDevice, Value: hostname}
		out = append(out, ent)
		seen[entityKey(ent)] = struct{}{}
	}

	if strings.TrimSpace(message) == "" {
		return out
	}

	lex := lexer.New(message)
	lex.SetParseMode(lexer.ParseModeShow)
	tokens := lex.Tokenize()

	for _, tok := range tokens {
		ent, ok := entityFromToken(tok, hostname)
		if !ok {
			continue
		}
		key := entityKey(ent)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ent)
	}

	// ASN regex fallback — jink's TokenASN only fires in config mode. Many
	// syslog messages reference ASNs as "AS65001" or "AS 65001".
	for _, m := range asnPattern.FindAllStringSubmatch(message, -1) {
		v, ok := normalizeASN(m[1])
		if !ok {
			continue
		}
		ent := Entity{Type: EntityASN, Value: v}
		key := entityKey(ent)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ent)
	}
	return out
}

func entityFromToken(tok lexer.Token, hostname string) (Entity, bool) {
	switch tok.Type {
	case lexer.TokenIPv4, lexer.TokenIPv6:
		v, ok := normalizeIP(tok.Value)
		if !ok {
			return Entity{}, false
		}
		return Entity{Type: EntityIP, Value: v}, true
	case lexer.TokenIPv4Prefix, lexer.TokenIPv6Prefix:
		v, ok := normalizePrefix(tok.Value)
		if !ok {
			return Entity{}, false
		}
		return Entity{Type: EntityPrefix, Value: v}, true
	case lexer.TokenASN:
		v, ok := normalizeASN(tok.Value)
		if !ok {
			return Entity{}, false
		}
		return Entity{Type: EntityASN, Value: v}, true
	case lexer.TokenInterface:
		v := strings.TrimSpace(tok.Value)
		if v == "" {
			return Entity{}, false
		}
		ent := Entity{Type: EntityInterface, Value: v}
		if hostname != "" {
			ent.Context = map[string]string{"device": hostname}
		}
		return ent, true
	default:
		return Entity{}, false
	}
}

func normalizeIP(s string) (string, bool) {
	addr, err := netip.ParseAddr(strings.TrimSpace(s))
	if err != nil {
		return "", false
	}
	return addr.String(), true
}

func normalizePrefix(s string) (string, bool) {
	p, err := netip.ParsePrefix(strings.TrimSpace(s))
	if err != nil {
		return "", false
	}
	return p.String(), true
}

func normalizeASN(s string) (string, bool) {
	v := strings.TrimSpace(s)
	v = strings.TrimPrefix(strings.ToUpper(v), "AS")
	v = strings.TrimSpace(v)
	if _, err := strconv.ParseUint(v, 10, 32); err != nil {
		return "", false
	}
	return v, true
}

func entityKey(e Entity) string {
	if e.Type == EntityInterface {
		return e.Type + ":" + e.Context["device"] + ":" + e.Value
	}
	return e.Type + ":" + e.Value
}
