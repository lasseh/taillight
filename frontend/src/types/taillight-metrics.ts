export interface TaillightMetricsSummary {
  sse_clients_syslog: number
  sse_clients_applog: number
  db_pool_active: number
  db_pool_idle: number
  db_pool_total: number
  events_broadcast: number
  events_dropped: number
  applog_events_broadcast: number
  applog_events_dropped: number
  applog_ingest_total: number
  applog_ingest_errors: number
  listener_reconnects: number
  events_rate: number
  ingest_rate: number
}

export interface TaillightMetricsSummaryResponse {
  data: TaillightMetricsSummary
}

export interface TaillightMetricsTimeSeries {
  time: string
  value: number
}

export interface TaillightMetricsVolumeResponse {
  data: TaillightMetricsTimeSeries[]
}
