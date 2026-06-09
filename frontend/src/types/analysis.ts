export type AnalysisFeed = 'netlog' | 'srvlog' | 'all'

export type AnalysisStatus = 'pending' | 'running' | 'completed' | 'failed'

// Prompt mode framing the report's narrative. Auto-derived from cadence for
// scheduled runs (daily cadence -> daily, weekly/monthly -> weekly). User-
// selectable for manual triggers; incident is only available manually.
export type AnalysisPromptMode = 'daily' | 'weekly' | 'incident'

export interface AnalysisReport {
  id: number
  slug: string
  feed: AnalysisFeed
  prompt_mode: AnalysisPromptMode
  // Host scope: empty array = "all hosts on the feed". Token-count contract
  // (prompt_tokens=0 && status='completed') indicates the analyzer
  // short-circuited because the window was empty; see backend docs.
  hosts: string[]
  model: string
  period_start: string
  period_end: string
  report?: string
  prompt_tokens: number
  completion_tokens: number
  status: AnalysisStatus
  error?: string
  created_at: string
  started_at?: string
  completed_at?: string
}

export interface AnalysisReportSummary {
  id: number
  slug: string
  feed: AnalysisFeed
  prompt_mode: AnalysisPromptMode
  hosts: string[]
  model: string
  period_start: string
  period_end: string
  prompt_tokens: number
  completion_tokens: number
  status: AnalysisStatus
  created_at: string
  started_at?: string
  completed_at?: string
}

export interface AnalysisReportListResponse {
  data: AnalysisReportSummary[]
}

export interface AnalysisReportResponse {
  data: AnalysisReport
}

export type AnalysisFrequency = 'daily' | 'weekly' | 'monthly'

export interface AnalysisSchedule {
  id: number
  name: string
  enabled: boolean
  feed: AnalysisFeed
  frequency: AnalysisFrequency
  day_of_week?: number
  day_of_month?: number
  time_of_day: string
  timezone: string
  // Email notification channel ids the completed report is mailed to.
  notify_channel_ids: number[]
  last_run_at?: string
  created_at: string
  updated_at: string
}

export interface AnalysisScheduleListResponse {
  data: AnalysisSchedule[]
}

export interface AnalysisScheduleResponse {
  data: AnalysisSchedule
}

export interface CreateAnalysisReportRequest {
  feed: AnalysisFeed
  // Optional. Empty/undefined defaults to "daily" on the server.
  prompt_mode?: AnalysisPromptMode
  // Optional. Empty/0 picks a mode-aware default: 1440 (daily), 10080 (weekly),
  // or 60 (incident). Bounds: 5..43200 (5 min..30 days).
  period_minutes?: number
  // Optional. Empty/undefined means "all hosts on the feed". Names are
  // validated against the feed's host metadata server-side; bad names
  // surface as a 400 unknown_hosts response.
  hosts?: string[]
}

// One row from GET /api/v1/analysis/hosts?feed=…, used by the picker to
// populate its autocomplete suggestions. last_seen is the most recent
// hour-aligned bucket the host appeared in (may be undefined for hosts
// that have entered the meta cache but not yet rolled into the aggregate).
export interface AnalysisHostEntry {
  hostname: string
  last_seen?: string
}

export interface AnalysisHostListResponse {
  data: AnalysisHostEntry[]
}

export type CreateAnalysisScheduleRequest = Omit<
  AnalysisSchedule,
  'id' | 'last_run_at' | 'created_at' | 'updated_at'
>
