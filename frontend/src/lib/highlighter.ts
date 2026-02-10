import Prism from 'prismjs'
import 'prismjs/components/prism-log'
import 'prismjs/components/prism-json'

// Extend the log grammar with Juniper/JunOS-specific tokens.
// insertBefore places these before 'string' so they get matched first.
Prism.languages.insertBefore('log', 'string', {
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
})

export function highlight(msg: string): string {
  return Prism.highlight(msg, Prism.languages['log']!, 'log')
}

export function highlightJson(obj: Record<string, unknown> | null): string {
  if (!obj) return ''
  const json = JSON.stringify(obj, null, 2)
  return Prism.highlight(json, Prism.languages['json']!, 'json')
}

const cache = new Map<number, string>()

export function highlightMessage(id: number, msg: string): string {
  let result = cache.get(id)
  if (result !== undefined) return result

  result = Prism.highlight(msg, Prism.languages['log']!, 'log')
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
