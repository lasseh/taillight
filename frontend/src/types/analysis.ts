export interface AnalysisReport {
  id: number
  generated_at: string
  model: string
  period_start: string
  period_end: string
  report: string
  prompt_tokens: number
  completion_tokens: number
  duration_ms: number
  status: string
}

export interface AnalysisReportSummary {
  id: number
  generated_at: string
  model: string
  period_start: string
  period_end: string
  prompt_tokens: number
  completion_tokens: number
  duration_ms: number
  status: string
}
