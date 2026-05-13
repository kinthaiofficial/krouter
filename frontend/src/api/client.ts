// All endpoints are relative to the daemon's management API on the same origin.
const BASE = ''

async function get<T>(path: string): Promise<T> {
  const resp = await fetch(BASE + path, { credentials: 'include' })
  if (resp.status === 401) throw new Error('unauthorized')
  if (!resp.ok) throw new Error(`HTTP ${resp.status}`)
  return resp.json() as Promise<T>
}

async function post<T>(path: string, body?: unknown): Promise<T> {
  const resp = await fetch(BASE + path, {
    method: 'POST',
    credentials: 'include',
    headers: body ? { 'Content-Type': 'application/json' } : {},
    body: body ? JSON.stringify(body) : undefined,
  })
  if (resp.status === 401) throw new Error('unauthorized')
  if (!resp.ok) throw new Error(`HTTP ${resp.status}`)
  return resp.json() as Promise<T>
}

async function patchReq<T>(path: string, body: unknown): Promise<T> {
  const resp = await fetch(BASE + path, {
    method: 'PATCH',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (resp.status === 401) throw new Error('unauthorized')
  if (!resp.ok) throw new Error(`HTTP ${resp.status}`)
  return resp.json() as Promise<T>
}

export type Preset = 'saver' | 'balanced' | 'quality'

export interface Settings {
  preset: Preset
  language: string
  notification_categories?: Record<string, boolean>
  budget_warnings?: Record<string, number>
}

export interface Usage {
  requests_today: number
  cost_today_usd: number
  savings_today_usd: number
}

export interface Budget {
  date: string
  requests_today: number
  cost_today_usd: number
  savings_today_usd: number
}

export interface QuotaItem {
  window: string
  tokens_used: number
  window_start: string
  window_end: string
  updated_at: string
}

export interface LogRecord {
  id: string
  ts: string
  agent?: string
  protocol: string
  requested_model?: string
  provider: string
  model: string
  input_tokens: number
  output_tokens: number
  cost_usd: number
  latency_ms: number
  status_code: number
  error_message?: string
}

export interface StatusResponse {
  status: string
  version: string
  uptime_seconds: number
  pid: number
  proxy_port: number
  mgmt_port: number
}

export const api = {
  status: () => get<StatusResponse>('/internal/status'),
  settings: () => get<Settings>('/internal/settings'),
  patchSettings: (changes: Partial<Settings>) => patchReq<Settings>('/internal/settings', changes),
  usage: () => get<Usage>('/internal/usage'),
  budget: () => get<Budget>('/internal/budget'),
  quota: () => get<QuotaItem[]>('/internal/quota'),
  logs: (n = 20) => get<LogRecord[]>(`/internal/logs?n=${n}`),
  preset: () => get<{ preset: Preset }>('/internal/preset'),
  setPreset: (preset: Preset) => post<{ preset: Preset }>('/internal/preset', { preset }),
}
