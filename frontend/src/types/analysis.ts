export type AnalysisFeed = 'netlog' | 'srvlog' | 'all'

export type AnalysisStatus = 'pending' | 'running' | 'completed' | 'failed'

export interface AnalysisReport {
  id: number
  slug: string
  feed: AnalysisFeed
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
}

export type CreateAnalysisScheduleRequest = Omit<
  AnalysisSchedule,
  'id' | 'last_run_at' | 'created_at' | 'updated_at'
>
