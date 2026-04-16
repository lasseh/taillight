import { Prism } from './prism-global'
import 'prismjs/components/prism-log'
import 'prismjs/components/prism-json'
import DOMPurify from 'dompurify'

// Extend the log grammar with IPv6 and Juniper/JunOS-specific tokens.
// insertBefore places these before 'string' so they get matched first.
Prism.languages.insertBefore('log', 'string', {
  // IPv6 addresses: [2a0f:b6c0:d102:1::26]:8080, fe80::1, 2001:db8::1
  // Must come before 'operator' to prevent colons being tokenized separately.
  'ipv6-address': {
    pattern:
      /\[[0-9a-f]{1,4}(?::[0-9a-f]{0,4}){1,7}\](?::\d{1,5})?|\b[0-9a-f]{1,4}(?::[0-9a-f]{0,4}){2,7}\b/i,
    alias: 'constant',
  },

  // JunOS syslog event tags: RPD_BGP_NEIGHBOR_STATE_CHANGED, UI_COMMIT, etc.
  'junos-event-tag': /\b[A-Z][A-Z0-9]+_[A-Z][A-Z0-9_]+\b/,

  // Interface names: ge-0/0/0, xe-1/2/3.100, ae0, lo0, irb.500, reth0, st0, vlan.100, etc.
  'junos-interface':
    /\b(?:(?:(?:[gx]e|et|so|fe|gr|ip|[lmuv]t|p[de]|pf[eh]|lc|lsq|sp)-\d+\/\d+\/\d+(?::\d+)?)|(?:ae|em|fxp|lo|me|vme|pp)\d{0,4}|(?:reth|irb|cbp|lsi|mtun|pimd|pime|tap|dsc|demux|st|vlan)\d*)(?:\.\d{1,5})?\b/,

  // Routing protocols
  'junos-protocol':
    /\b(?:BGP|OSPF|OSPFv[23]|IS-?IS|MPLS|LDP|RSVP|BFD|LACP|LLDP|VRRP|RIP(?:ng)?|PIM|IGMP|MLD|MSDP|STP|RSTP|MSTP|MVRP)\b/,

  // BGP states
  'junos-bgp-state':
    /\b(?:Idle|Connect|Active|OpenSent|OpenConfirm|Established)\b/,

  // Firewall / security actions
  'junos-action': {
    pattern: /\b(?:accept|permit|deny|discard|reject)\b/i,
    alias: 'keyword',
  },

  // Hardware components
  'junos-hardware':
    /\b(?:FPC|PIC|MIC|MPC|RE[01]?|SCB|SFB|SIB|CB|FEB|PEM|PSU|routing-engine|line-card)\b/,

  // JunOS daemon process names
  'junos-process':
    /\b(?:rpd|chassisd|mgd|dcd|pfed|dfwd|snmpd|mib2d|alarmd|craftd|eventd|cosd|ppmd|vrrpd|bfdd|sampled|kmd|l2ald|eswd|lacpd|rmopd|lmpd|fsad|spd|authd|jdhcpd|l2cpd|sflowd|lfmd|ksyncd|xntpd|ntpd|apsd|ilmid|irsd|nasd|fud|rdd|sdxd|jdiameterd|mcsnoopd|vccpd|sendd)\b/,

  // Routing table names: inet.0, inet6.0, bgp.l3vpn.0, mpls.0
  'junos-table':
    /\b(?:[\w-]+\.)?(?:inet6?|mpls|inetflow|iso|bgp\.l[23]vpn)\.\d+\b/,

  // ASN references: AS64512, AS 174
  'junos-asn': /\bAS\s?\d{1,10}\b/,

  // --- State indicators (from jink/cink) ---

  // Good states: up, established, forwarding, online, etc.
  'state-good': {
    pattern: /\b(?:up|Up|UP|Established|established|Establ|establ|Full|full|forwarding|Forwarding|learning|Learning|Master|master|Primary|primary|Enabled|enabled|online|Online|running|Running|ready|Ready|ok|OK|Ok|connected|Connected)\b/,
    alias: 'state-good',
  },

  // Bad states: down, idle, failed, error, disabled, etc.
  'state-bad': {
    pattern: /\b(?:down|Down|DOWN|Idle|idle|failed|Failed|FAILED|error|Error|ERROR|offline|Offline|disabled|Disabled|OpenSent|OpenConfirm|flapping|Flapping|errdisabled|blocked|Blocked|unreachable|Unreachable)\b/,
    alias: 'state-bad',
  },

  // Warning/transitional states: discarding, init, exchange, etc.
  'state-warning': {
    pattern: /\b(?:discarding|Discarding|ExStart|Exchange|Loading|2Way|Init|init|degraded|Degraded|standby|Standby|backup|Backup|inactive|Inactive|warning|Warning)\b/,
    alias: 'state-warning',
  },

  // --- Multi-vendor interface names ---

  // Cisco/Arista interfaces: Ethernet1/2, GigabitEthernet0/0/0, Vlan100, Loopback0, Port-channel1, Vxlan1
  'eos-interface':
    /\b(?:(?:Ethernet|GigabitEthernet|FastEthernet|TenGigabitEthernet|TwentyFiveGigE|FortyGigabitEthernet|HundredGigE|Gi|Fa|Te|Twe|Fo|Hu|Et)\d+(?:\/\d+)*(?:\.\d+)?|(?:Vlan|Loopback|Port-[Cc]hannel|Vxlan|Management|Tunnel|BDI|nve)\d+)\b/,

  // MAC addresses: 00:5f:67:52:ba:0d or 0011.2233.4455
  'mac-address':
    /\b(?:[0-9a-fA-F]{2}(?::[0-9a-fA-F]{2}){5}|[0-9a-fA-F]{4}\.[0-9a-fA-F]{4}\.[0-9a-fA-F]{4})\b/,

  // VLAN references: vlan 100, VLAN 4094
  'vlan-id': {
    pattern: /\b[Vv][Ll][Aa][Nn]\s+\d{1,4}\b/,
    alias: 'vlan-id',
  },

  // Arista/Cisco event tags: %BFD-5-STATE_CHANGE
  'eos-event-tag': /\B%[A-Z][A-Z0-9_]*-[0-9]+-[A-Z][A-Z0-9_]+\b/,
})

// Prism.highlight only emits <span class="...">...</span>; constrain DOMPurify to
// match so anything unexpected (e.g. a bad grammar extension) can't introduce attrs.
const PRISM_SANITIZE = { ALLOWED_TAGS: ['span'], ALLOWED_ATTR: ['class'] }

export function highlight(msg: string): string {
  return DOMPurify.sanitize(Prism.highlight(msg, Prism.languages['log']!, 'log'), PRISM_SANITIZE)
}

export function highlightJson(obj: Record<string, unknown> | null): string {
  if (!obj) return ''
  const json = JSON.stringify(obj, null, 2)
  return DOMPurify.sanitize(Prism.highlight(json, Prism.languages['json']!, 'json'), PRISM_SANITIZE)
}

const cache = new Map<number, string>()

export function highlightMessage(id: number, msg: string): string {
  let result = cache.get(id)
  if (result !== undefined) return result

  result = DOMPurify.sanitize(Prism.highlight(msg, Prism.languages['log']!, 'log'), PRISM_SANITIZE)
  cache.set(id, result)

  // Batch-evict oldest 500 entries when cache exceeds 3000 to avoid
  // running eviction on every subsequent insert.
  if (cache.size > 3000) {
    const iter = cache.keys()
    for (let i = 0; i < 500; i++) {
      const key = iter.next().value
      if (key !== undefined) cache.delete(key)
    }
  }

  return result
}
