import type { SrvlogListResponse, JuniperNetlogRef, MetaResponse, SingleSrvlogResponse } from '@/types/srvlog'
import type { NetlogListResponse, SingleNetlogResponse } from '@/types/netlog'
import type { AppLogListResponse, SingleAppLogResponse } from '@/types/applog'
import type { VolumeResponse, SeverityVolumeResponse, SrvlogSummaryResponse, AppLogSummaryResponse } from '@/types/stats'
import type { RsyslogStatsSummaryResponse, RsyslogStatsVolumeResponse } from '@/types/rsyslog-stats'
import type { TaillightMetricsSummaryResponse, TaillightMetricsVolumeResponse } from '@/types/taillight-metrics'
import type { LoginResponse, MeResponse, ListKeysResponse, CreateKeyRequest, CreateKeyResponse, ListUsersResponse, AdminUser } from '@/types/auth'
import type { ChannelListResponse, ChannelResponse, RuleListResponse, RuleResponse, LogListResponse, TestChannelResult, NotificationChannel, NotificationRule } from '@/types/notification'
import type { DeviceSummaryResponse, AppLogDeviceSummaryResponse } from '@/types/device'
import type { HostsResponse } from '@/types/host'
import type { AnalysisReportListResponse, AnalysisReportResponse, AnalysisTriggerResponse } from '@/types/analysis'
import { config } from './config'

/** Shape of the JSON error body returned by the API. */
interface ApiErrorBody {
  error?: {
    code?: string
    message?: string
  }
}

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

async function handleResponse<T>(res: Response): Promise<T> {
  if (!res.ok) {
    const body: ApiErrorBody | null = await res.json().catch(() => null)
    const code = body?.error?.code ?? 'unknown'
    const message = body?.error?.message ?? res.statusText
    throw new ApiError(res.status, code, message)
  }
  if (res.status === 204) return undefined as T
  return res.json()
}

async function fetchAPI<T>(path: string, signal?: AbortSignal): Promise<T> {
  const url = `${config.apiUrl}${path}`
  const res = await fetch(url, {
    signal: signal ?? AbortSignal.timeout(15000),
    credentials: 'include',
  })
  return handleResponse(res)
}

async function postAPI<T>(path: string, body: unknown, signal?: AbortSignal): Promise<T> {
  const url = `${config.apiUrl}${path}`
  const res = await fetch(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
    signal: signal ?? AbortSignal.timeout(15000),
    credentials: 'include',
  })
  return handleResponse(res)
}

async function deleteAPI<T>(path: string): Promise<T> {
  const url = `${config.apiUrl}${path}`
  const res = await fetch(url, {
    method: 'DELETE',
    signal: AbortSignal.timeout(15000),
    credentials: 'include',
  })
  return handleResponse(res)
}

async function patchAPI<T>(path: string, body: unknown): Promise<T> {
  const url = `${config.apiUrl}${path}`
  const res = await fetch(url, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
    signal: AbortSignal.timeout(15000),
    credentials: 'include',
  })
  return handleResponse(res)
}

async function putAPI<T>(path: string, body: unknown): Promise<T> {
  const url = `${config.apiUrl}${path}`
  const res = await fetch(url, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
    signal: AbortSignal.timeout(15000),
    credentials: 'include',
  })
  return handleResponse(res)
}

export const api = {
  // Auth
  login(username: string, password: string): Promise<LoginResponse> {
    return postAPI('/api/v1/auth/login', { username, password })
  },

  logout(): Promise<{ status: string }> {
    return postAPI('/api/v1/auth/logout', {})
  },

  getMe(): Promise<MeResponse> {
    return fetchAPI('/api/v1/auth/me')
  },

  listKeys(): Promise<ListKeysResponse> {
    return fetchAPI('/api/v1/auth/keys')
  },

  createKey(req: CreateKeyRequest): Promise<CreateKeyResponse> {
    return postAPI('/api/v1/auth/keys', req)
  },

  revokeKey(id: string): Promise<{ status: string }> {
    return deleteAPI(`/api/v1/auth/keys/${id}`)
  },

  updatePassword(id: string, password: string, currentPassword: string): Promise<{ status: string }> {
    return patchAPI(`/api/v1/auth/users/${id}/password`, { password, current_password: currentPassword })
  },

  updateEmail(email: string): Promise<MeResponse> {
    return patchAPI('/api/v1/auth/me/email', { email })
  },

  // Admin user management
  listUsers(): Promise<ListUsersResponse> {
    return fetchAPI('/api/v1/auth/users')
  },

  createUser(req: { username: string; password: string; is_admin: boolean }): Promise<{ user: AdminUser }> {
    return postAPI('/api/v1/auth/users', req)
  },

  setUserActive(id: string, active: boolean): Promise<{ status: string }> {
    return patchAPI(`/api/v1/auth/users/${id}/active`, { active })
  },

  revokeUserSessions(id: string): Promise<{ status: string }> {
    return postAPI(`/api/v1/auth/users/${id}/revoke-sessions`, {})
  },

  adminResetPassword(id: string, password: string): Promise<{ status: string }> {
    return patchAPI(`/api/v1/auth/users/${id}/password`, { password })
  },

  // Srvlog
  getSrvlogs(params: URLSearchParams, signal?: AbortSignal): Promise<SrvlogListResponse> {
    return fetchAPI(`/api/v1/srvlog?${params}`, signal)
  },

  getSrvlog(id: number): Promise<SingleSrvlogResponse> {
    return fetchAPI(`/api/v1/srvlog/${id}`)
  },

  getHosts(): Promise<MetaResponse<string>> {
    return fetchAPI('/api/v1/srvlog/meta/hosts')
  },

  getPrograms(): Promise<MetaResponse<string>> {
    return fetchAPI('/api/v1/srvlog/meta/programs')
  },

  getFacilities(): Promise<MetaResponse<number>> {
    return fetchAPI('/api/v1/srvlog/meta/facilities')
  },

  getTags(): Promise<MetaResponse<string>> {
    return fetchAPI('/api/v1/srvlog/meta/tags')
  },

  getSrvlogVolume(params: URLSearchParams): Promise<VolumeResponse> {
    return fetchAPI(`/api/v1/srvlog/stats/volume?${params}`)
  },

  getJuniperLookup(name: string): Promise<MetaResponse<JuniperNetlogRef>> {
    return fetchAPI(`/api/v1/juniper/lookup?name=${encodeURIComponent(name)}`)
  },

  // Netlog
  getNetlogs(params: URLSearchParams, signal?: AbortSignal): Promise<NetlogListResponse> {
    return fetchAPI(`/api/v1/netlog?${params}`, signal)
  },

  getNetlog(id: number): Promise<SingleNetlogResponse> {
    return fetchAPI(`/api/v1/netlog/${id}`)
  },

  getNetlogHosts(): Promise<MetaResponse<string>> {
    return fetchAPI('/api/v1/netlog/meta/hosts')
  },

  getNetlogPrograms(): Promise<MetaResponse<string>> {
    return fetchAPI('/api/v1/netlog/meta/programs')
  },

  getNetlogFacilities(): Promise<MetaResponse<number>> {
    return fetchAPI('/api/v1/netlog/meta/facilities')
  },

  getNetlogTags(): Promise<MetaResponse<string>> {
    return fetchAPI('/api/v1/netlog/meta/tags')
  },

  getNetlogVolume(params: URLSearchParams): Promise<VolumeResponse> {
    return fetchAPI(`/api/v1/netlog/stats/volume?${params}`)
  },

  getNetlogSeverityVolume(params: URLSearchParams): Promise<SeverityVolumeResponse> {
    return fetchAPI(`/api/v1/netlog/stats/severity-volume?${params}`)
  },

  getNetlogDeviceSummary(hostname: string): Promise<DeviceSummaryResponse> {
    return fetchAPI(`/api/v1/netlog/device/${encodeURIComponent(hostname)}`)
  },

  getNetlogSummary(range?: string): Promise<SrvlogSummaryResponse> {
    const q = range ? `?range=${encodeURIComponent(range)}` : ''
    return fetchAPI(`/api/v1/netlog/stats/summary${q}`)
  },

  // App log
  getAppLogs(params: URLSearchParams, signal?: AbortSignal): Promise<AppLogListResponse> {
    return fetchAPI(`/api/v1/applog?${params}`, signal)
  },

  getAppLog(id: number): Promise<SingleAppLogResponse> {
    return fetchAPI(`/api/v1/applog/${id}`)
  },

  getAppLogServices(): Promise<MetaResponse<string>> {
    return fetchAPI('/api/v1/applog/meta/services')
  },

  getAppLogComponents(): Promise<MetaResponse<string>> {
    return fetchAPI('/api/v1/applog/meta/components')
  },

  getAppLogHosts(): Promise<MetaResponse<string>> {
    return fetchAPI('/api/v1/applog/meta/hosts')
  },

  getAppLogVolume(params: URLSearchParams): Promise<VolumeResponse> {
    return fetchAPI(`/api/v1/applog/stats/volume?${params}`)
  },

  getSrvlogSeverityVolume(params: URLSearchParams): Promise<SeverityVolumeResponse> {
    return fetchAPI(`/api/v1/srvlog/stats/severity-volume?${params}`)
  },

  getAppLogSeverityVolume(params: URLSearchParams): Promise<SeverityVolumeResponse> {
    return fetchAPI(`/api/v1/applog/stats/severity-volume?${params}`)
  },

  // Device
  getSrvlogDeviceSummary(hostname: string): Promise<DeviceSummaryResponse> {
    return fetchAPI(`/api/v1/srvlog/device/${encodeURIComponent(hostname)}`)
  },

  getAppLogDeviceSummary(hostname: string): Promise<AppLogDeviceSummaryResponse> {
    return fetchAPI(`/api/v1/applog/device/${encodeURIComponent(hostname)}`)
  },

  getSrvlogSummary(range?: string): Promise<SrvlogSummaryResponse> {
    const q = range ? `?range=${encodeURIComponent(range)}` : ''
    return fetchAPI(`/api/v1/srvlog/stats/summary${q}`)
  },

  getHostsSummary(range?: string): Promise<HostsResponse> {
    const q = range ? `?range=${encodeURIComponent(range)}` : ''
    return fetchAPI(`/api/v1/srvlog/stats/hosts${q}`)
  },

  getAppLogSummary(range?: string): Promise<AppLogSummaryResponse> {
    const q = range ? `?range=${encodeURIComponent(range)}` : ''
    return fetchAPI(`/api/v1/applog/stats/summary${q}`)
  },

  // Rsyslog stats
  getRsyslogStatsSummary(range?: string): Promise<RsyslogStatsSummaryResponse> {
    const q = range ? `?range=${encodeURIComponent(range)}` : ''
    return fetchAPI(`/api/v1/rsyslog/stats/summary${q}`)
  },

  getRsyslogStatsVolume(params: URLSearchParams): Promise<RsyslogStatsVolumeResponse> {
    return fetchAPI(`/api/v1/rsyslog/stats/volume?${params}`)
  },

  // Taillight metrics
  getTaillightMetricsSummary(range?: string): Promise<TaillightMetricsSummaryResponse> {
    const q = range ? `?range=${encodeURIComponent(range)}` : ''
    return fetchAPI(`/api/v1/metrics/summary${q}`)
  },

  getTaillightMetricsVolume(params: URLSearchParams): Promise<TaillightMetricsVolumeResponse> {
    return fetchAPI(`/api/v1/metrics/volume?${params}`)
  },

  // Notifications
  listChannels(): Promise<ChannelListResponse> {
    return fetchAPI('/api/v1/notifications/channels')
  },

  getChannel(id: number): Promise<ChannelResponse> {
    return fetchAPI(`/api/v1/notifications/channels/${id}`)
  },

  createChannel(ch: Partial<NotificationChannel>): Promise<ChannelResponse> {
    return postAPI('/api/v1/notifications/channels', ch)
  },

  updateChannel(id: number, ch: Partial<NotificationChannel>): Promise<ChannelResponse> {
    return putAPI(`/api/v1/notifications/channels/${id}`, ch)
  },

  deleteChannel(id: number): Promise<void> {
    return deleteAPI(`/api/v1/notifications/channels/${id}`)
  },

  testChannel(id: number): Promise<TestChannelResult> {
    return postAPI(`/api/v1/notifications/channels/${id}/test`, {})
  },

  listRules(): Promise<RuleListResponse> {
    return fetchAPI('/api/v1/notifications/rules')
  },

  getRule(id: number): Promise<RuleResponse> {
    return fetchAPI(`/api/v1/notifications/rules/${id}`)
  },

  createRule(rule: Partial<NotificationRule>): Promise<RuleResponse> {
    return postAPI('/api/v1/notifications/rules', rule)
  },

  updateRule(id: number, rule: Partial<NotificationRule>): Promise<RuleResponse> {
    return putAPI(`/api/v1/notifications/rules/${id}`, rule)
  },

  deleteRule(id: number): Promise<void> {
    return deleteAPI(`/api/v1/notifications/rules/${id}`)
  },

  listNotificationLog(params: URLSearchParams): Promise<LogListResponse> {
    return fetchAPI(`/api/v1/notifications/log?${params}`)
  },

  // Analysis
  listAnalysisReports(limit?: number): Promise<AnalysisReportListResponse> {
    const q = limit ? `?limit=${limit}` : ''
    return fetchAPI(`/api/v1/analysis/reports${q}`)
  },

  getAnalysisReport(id: number): Promise<AnalysisReportResponse> {
    return fetchAPI(`/api/v1/analysis/reports/${id}`)
  },

  getLatestAnalysisReport(): Promise<AnalysisReportResponse> {
    return fetchAPI('/api/v1/analysis/reports/latest')
  },

  triggerAnalysis(signal?: AbortSignal): Promise<AnalysisTriggerResponse> {
    return postAPI('/api/v1/analysis/reports/trigger', {}, signal)
  },
}
