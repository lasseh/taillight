import type { SyslogListResponse, JuniperSyslogRef, MetaResponse, SingleSyslogResponse } from '@/types/syslog'
import type { AppLogListResponse, SingleAppLogResponse } from '@/types/applog'
import type { VolumeResponse, SyslogSummaryResponse, AppLogSummaryResponse } from '@/types/stats'
import type { RsyslogStatsSummaryResponse, RsyslogStatsVolumeResponse } from '@/types/rsyslog-stats'
import type { TaillightMetricsSummaryResponse, TaillightMetricsVolumeResponse } from '@/types/taillight-metrics'
import type { LoginResponse, MeResponse, ListKeysResponse, CreateKeyRequest, CreateKeyResponse } from '@/types/auth'
import type { ChannelListResponse, ChannelResponse, RuleListResponse, RuleResponse, LogListResponse, TestChannelResult, NotificationChannel, NotificationRule } from '@/types/notification'
import type { DeviceSummaryResponse } from '@/types/device'
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
    const message = body?.error?.message ?? `HTTP ${res.status}`
    throw new ApiError(res.status, code, message)
  }
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

async function postAPI<T>(path: string, body: unknown): Promise<T> {
  const url = `${config.apiUrl}${path}`
  const res = await fetch(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
    signal: AbortSignal.timeout(15000),
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

  // Syslog
  getSyslogs(params: URLSearchParams, signal?: AbortSignal): Promise<SyslogListResponse> {
    return fetchAPI(`/api/v1/syslog?${params}`, signal)
  },

  getSyslog(id: number): Promise<SingleSyslogResponse> {
    return fetchAPI(`/api/v1/syslog/${id}`)
  },

  getHosts(): Promise<MetaResponse<string>> {
    return fetchAPI('/api/v1/meta/hosts')
  },

  getPrograms(): Promise<MetaResponse<string>> {
    return fetchAPI('/api/v1/meta/programs')
  },

  getFacilities(): Promise<MetaResponse<number>> {
    return fetchAPI('/api/v1/meta/facilities')
  },

  getTags(): Promise<MetaResponse<string>> {
    return fetchAPI('/api/v1/meta/tags')
  },

  getVolume(params: URLSearchParams): Promise<VolumeResponse> {
    return fetchAPI(`/api/v1/stats/volume?${params}`)
  },

  getJuniperLookup(name: string): Promise<MetaResponse<JuniperSyslogRef>> {
    return fetchAPI(`/api/v1/juniper/lookup?name=${encodeURIComponent(name)}`)
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

  // Device
  getDeviceSummary(hostname: string): Promise<DeviceSummaryResponse> {
    return fetchAPI(`/api/v1/device/${encodeURIComponent(hostname)}`)
  },

  getSyslogSummary(range?: string): Promise<SyslogSummaryResponse> {
    const q = range ? `?range=${encodeURIComponent(range)}` : ''
    return fetchAPI(`/api/v1/stats/summary${q}`)
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
}
