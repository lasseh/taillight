export interface VolumeBucket {
  time: string
  total: number
  by_host: Record<string, number>
}

export interface VolumeResponse {
  data: VolumeBucket[]
}

/** Flat record for Unovis charts: x (epoch ms), total, and dynamic host keys. */
export interface VolumeDataRecord {
  x: number
  total: number
  [host: string]: number
}

export interface SeverityCount {
  severity: number
  label: string
  count: number
  pct: number
}

export interface LevelCount {
  level: string
  count: number
  pct: number
}

export interface TopSource {
  name: string
  count: number
  pct: number
}

export interface SyslogSummary {
  total: number
  trend: number
  errors: number
  warnings: number
  severity_breakdown: SeverityCount[]
  top_hosts: TopSource[]
}

export interface AppLogSummary {
  total: number
  trend: number
  errors: number
  warnings: number
  level_breakdown: LevelCount[]
  top_services: TopSource[]
}

export interface SeverityVolumeBucket {
  time: string
  total: number
  by_severity: Record<string, number>
}

export interface SeverityVolumeResponse {
  data: SeverityVolumeBucket[]
}

export interface SyslogSummaryResponse {
  data: SyslogSummary
}

export interface AppLogSummaryResponse {
  data: AppLogSummary
}
