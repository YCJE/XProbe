export type TrafficMonthly = {
  month: string
  rx_bytes: number
  tx_bytes: number
}

export type Agent = {
  id: number
  hostname: string
  display_name: string
  os: string
  arch: string
  agent_version: string
  online: boolean
  last_seen?: number
  ipv4?: string
  ipv6?: string
  region?: string
  country_code?: string
  isp?: string
  cpu_usage?: number
  mem_usage?: number
  disk_usage?: number
  rx_speed?: number
  tx_speed?: number
  uptime?: number
  load_1?: number
  load_5?: number
  load_15?: number
  traffic_quota_bytes?: number
  traffic_monthly?: TrafficMonthly
  expires_at?: number
  price_amount?: number
  price_currency?: string
  price_cycle?: string
}

export type DiskInfo = {
  device: string
  total: number
  used: number
  percent: number
  mount?: string
}

export type ServerDetail = Agent & {
  memory_total?: number
  memory_used?: number
  swap_total?: number
  swap_used?: number
  process_count?: number
  tcp_connections?: number
  udp_connections?: number
  disks?: DiskInfo[]
  tags?: string[]
}

export type MetricPoint = {
  timestamp: number
  cpu: number
  mem: number
  disk: number
  net_rx: number
  net_tx: number
}

export type PingPoint = {
  timestamp: number
  avg: number
  min?: number
  max?: number
  jitter?: number
  loss: number
  method?: string
  ip_version?: number
}

export type PingSeries = {
  target: string
  name: string
  method: string
  ip_version: number
  points: PingPoint[]
}

export type History = {
  range: string
  metrics: MetricPoint[]
  pings: PingSeries[]
}

export type Session = {
  username: string
  session_id: string
  expires_at: number
  idle_expires_at: number
}

export type SetupStatus = {
  needs_setup: boolean
}

export type RegisterCode = {
  id: number
  code: string
  expires_at: number
}

export type AlertScope = {
  type: string
  ids?: number[]
}

export type AlertRule = {
  id: number
  name: string
  metric: string
  operator: string
  threshold: number
  duration: number
  enabled: boolean
  scope: AlertScope
  notify_channel_ids: number[]
  created_at?: string
}

export type AlertHistory = {
  id: number
  rule_id: number
  agent_id: number
  status: string
  value: number
  started_at: string
  updated_at: string
  notified: boolean
}

export type NotifyChannel = {
  id: number
  name: string
  type: string
  config?: Record<string, unknown>
}

export type AuditEntry = {
  id: number
  timestamp: string
  actor: string
  action: string
  target: string
  detail: string
}

export type PingTargetConfig = {
  id: number
  target: string
  name: string
  region: string
  isp: string
  ip_version: number
  protocol: string
  is_default: boolean
  enabled: boolean
}

export type Settings = {
  theme?: string
  public_enabled?: boolean
  public_share_id?: string
  public_url?: string
  public_title?: string
  totp_enabled?: boolean
}

export type TotpSetup = {
  secret: string
  url: string
  recovery_codes?: string[]
}
