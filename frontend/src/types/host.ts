import type { SeverityCount, TopSource } from '@/types/stats'

export interface HourlyBucket {
  bucket: string
  count: number
  error_count: number
}

export interface HostEntry {
  hostname: string
  feed: 'srvlog' | 'netlog' | 'both'
  status: 'healthy' | 'warning' | 'critical'
  last_seen_at: string | null
  total_count: number
  error_count: number
  trend: number
  severity_breakdown: SeverityCount[]
  hourly_buckets: HourlyBucket[]
  top_errors: TopSource[]
}

export interface HostsResponse {
  data: HostEntry[]
}
