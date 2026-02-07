export interface RsyslogStatsComponent {
  collected_at: string
  origin: string
  name: string
  stats: Record<string, number>
}

export interface RsyslogStatsSummary {
  total_submitted: number
  total_processed: number
  total_failed: number
  total_suspended: number
  main_queue_size: number
  main_queue_max_size: number
  total_discarded: number
  filter_rate: number
  failure_rate: number
  ingest_rate: number
  components: RsyslogStatsComponent[]
}

export interface RsyslogStatsSummaryResponse {
  data: RsyslogStatsSummary
}

export interface RsyslogStatsTimeSeries {
  time: string
  name: string
  value: number
}

export interface RsyslogStatsVolumeResponse {
  data: RsyslogStatsTimeSeries[]
}

/** Flat record for Unovis charts: x (epoch ms) and dynamic name keys. */
export interface RsyslogStatsDataRecord {
  x: number
  [name: string]: number
}
