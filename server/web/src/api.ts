import type {
  Agent, AlertHistory, AlertRule, AuditEntry, History, NotifyChannel,
  PingTargetConfig, RegisterCode, ServerDetail, Session, Settings, SetupStatus, TotpSetup,
} from './types'

export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

let unauthorizedHandler: (() => void) | null = null

// App registers this once so every protected request, not just page loads,
// reacts to an expired or revoked session.
export function setUnauthorizedHandler(handler: (() => void) | null) {
  unauthorizedHandler = handler
}

function notifiesUnauthorized(path: string): boolean {
  return !path.startsWith('/api/v1/public/') &&
    path !== '/api/v1/auth/login' &&
    path !== '/api/v1/setup' &&
    path !== '/api/v1/setup/status'
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers)
  if (init.body && !headers.has('Content-Type')) headers.set('Content-Type', 'application/json')
  const controller = new AbortController()
  const timer = window.setTimeout(() => controller.abort(), 15_000)
  let res: Response
  try {
    res = await fetch(path, {
      ...init,
      headers,
      credentials: 'same-origin',
      cache: 'no-store',
      signal: controller.signal,
    })
  } finally {
    window.clearTimeout(timer)
  }
  if (res.status === 204) return undefined as T
  const text = await res.text()
  let data: unknown = null
  try { data = text ? JSON.parse(text) : null } catch { data = text }
  if (!res.ok) {
    if (res.status === 401 && notifiesUnauthorized(path)) {
      unauthorizedHandler?.()
    }
    const message = data && typeof data === 'object' && 'error' in data
      ? String((data as { error: unknown }).error)
      : res.statusText || '请求失败'
    throw new ApiError(res.status, message)
  }
  return data as T
}

const json = (value: unknown): RequestInit => ({ body: JSON.stringify(value) })

export const api = {
  setupStatus: () => request<SetupStatus>('/api/v1/setup/status'),
  setup: (token: string, password: string) =>
    request<{ ok: boolean }>('/api/v1/setup', { method: 'POST', ...json({ token, password }) }),
  login: (username: string, password: string, totpCode?: string) =>
    request<Session & { ok: boolean }>('/api/v1/auth/login', {
      method: 'POST', ...json({ username, password, totp_code: totpCode || '' }),
    }),
  logout: () => request<void>('/api/v1/auth/logout', { method: 'POST' }),
  me: () => request<Session>('/api/v1/auth/me'),

  servers: () => request<{ servers: Agent[] }>('/api/v1/servers'),
  server: (id: number) => request<ServerDetail>(`/api/v1/servers/${id}`),
  history: (id: number, range: string) =>
    request<History>(`/api/v1/servers/${id}/history?range=${encodeURIComponent(range)}`),
  updateServerMeta: (id: number, meta: {
    display_name: string; region: string; country_code: string; isp: string
    expires_at: number; price_amount: number; price_currency: string
    price_cycle: string; traffic_quota_bytes: number
  }) => request<void>(`/api/v1/servers/${id}/meta`, { method: 'PUT', ...json(meta) }),

  createRegisterCode: () =>
    request<RegisterCode>('/api/v1/agents/register-codes', { method: 'POST' }),
  registerCodes: () =>
    request<{ codes: Array<{ id: number; created_at: number; expires_at: number; used: boolean }> }>(
      '/api/v1/agents/register-codes',
    ),
  deleteRegisterCode: (id: number) =>
    request<void>(`/api/v1/agents/register-codes/${id}`, { method: 'DELETE' }),

  alerts: () => request<{ rules: AlertRule[] }>('/api/v1/alerts'),
  alertHistory: () => request<{ history: AlertHistory[] }>('/api/v1/alerts/history'),
  createAlert: (rule: Omit<AlertRule, 'id'>) =>
    request<AlertRule>('/api/v1/alerts', { method: 'POST', ...json(rule) }),
  updateAlert: (id: number, rule: Omit<AlertRule, 'id'>) =>
    request<AlertRule>(`/api/v1/alerts/${id}`, { method: 'PUT', ...json(rule) }),
  deleteAlert: (id: number) => request<void>(`/api/v1/alerts/${id}`, { method: 'DELETE' }),

  channels: () => request<{ channels: NotifyChannel[] }>('/api/v1/notify/channels'),
  createChannel: (channel: { name: string; type: string; config: Record<string, unknown> }) =>
    request<NotifyChannel>('/api/v1/notify/channels', { method: 'POST', ...json(channel) }),
  deleteChannel: (id: number) =>
    request<void>(`/api/v1/notify/channels/${id}`, { method: 'DELETE' }),
  testChannel: (id: number) =>
    request<{ ok: boolean }>(`/api/v1/notify/channels/${id}/test`, { method: 'POST' }),

  settings: () => request<Settings>('/api/v1/config/settings'),
  saveSettings: (settings: Settings) =>
    request<Settings>('/api/v1/config/settings', { method: 'PUT', ...json(settings) }),

  pingTargets: () => request<{ targets: PingTargetConfig[] }>('/api/v1/config/ping-targets'),
  createPingTarget: (target: Omit<PingTargetConfig, 'id' | 'is_default'>) =>
    request<PingTargetConfig>('/api/v1/config/ping-targets', { method: 'POST', ...json(target) }),
  updatePingTarget: (id: number, target: Omit<PingTargetConfig, 'id' | 'is_default'>) =>
    request<PingTargetConfig>(`/api/v1/config/ping-targets/${id}`, { method: 'PUT', ...json(target) }),
  deletePingTarget: (id: number) =>
    request<void>(`/api/v1/config/ping-targets/${id}`, { method: 'DELETE' }),

  totpSetup: (password: string) =>
    request<TotpSetup>('/api/v1/auth/totp/setup', { method: 'POST', ...json({ password }) }),
  totpEnable: (code: string) =>
    request<{ ok: boolean; recovery_codes: string[] }>('/api/v1/auth/totp/enable', {
      method: 'POST', ...json({ code }),
    }),
  totpDisable: (password: string) =>
    request<{ ok: boolean }>('/api/v1/auth/totp/disable', { method: 'POST', ...json({ password }) }),

  sessions: () => request<{ sessions: Array<Record<string, unknown>> }>('/api/v1/sessions'),
  deleteSession: (id: string) => request<void>(`/api/v1/sessions/${id}`, { method: 'DELETE' }),
  audit: () => request<{ events: AuditEntry[] }>('/api/v1/audit'),

  publicDashboard: (shareId: string) =>
    request<{ title: string; servers: Agent[] }>(`/api/v1/public/${encodeURIComponent(shareId)}`),
}

export function dashboardSocketURL(): string {
  const scheme = location.protocol === 'https:' ? 'wss' : 'ws'
  return `${scheme}://${location.host}/ws/dashboard`
}
