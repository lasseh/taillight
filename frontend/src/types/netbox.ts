// Mirrors the Go shapes in api/internal/netbox/types.go.

export type NetboxEntityType = 'device' | 'ip' | 'prefix' | 'asn' | 'interface'

export interface NetboxEntity {
  type: NetboxEntityType
  value: string
  context?: Record<string, string>
}

export interface NetboxDeviceData {
  id: number
  name: string
  status?: string
  role?: string
  site?: string
  device_type?: string
  manufacturer?: string
  primary_ip?: string
  description?: string
  url?: string
}

export interface NetboxIPData {
  id: number
  address: string
  status?: string
  role?: string
  dns_name?: string
  device?: string
  interface?: string
  vrf?: string
  description?: string
  url?: string
}

export interface NetboxPrefixData {
  id: number
  prefix: string
  status?: string
  role?: string
  site?: string
  vlan?: string
  vrf?: string
  description?: string
  url?: string
}

export interface NetboxASNData {
  id: number
  asn: number
  description?: string
  rir?: string
  url?: string
}

export interface NetboxInterfaceData {
  id: number
  name: string
  device?: string
  type?: string
  mtu?: number
  mac_address?: string
  description?: string
  enabled?: boolean
  lag?: string
  connected_endpoint?: string
  url?: string
}

export interface NetboxLookupData {
  device?: NetboxDeviceData
  ip?: NetboxIPData
  prefix?: NetboxPrefixData
  asn?: NetboxASNData
  interface?: NetboxInterfaceData
}

export interface NetboxLookup {
  entity: NetboxEntity
  found: boolean
  data?: NetboxLookupData
  error?: string
}

export interface NetboxEnrichmentResponse {
  data: {
    entities: NetboxEntity[]
    lookups: NetboxLookup[]
  }
}
