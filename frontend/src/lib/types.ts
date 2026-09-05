export interface ServerInfo {
  id: number;
  hostname: string;
  display_name: string;
  online: boolean;
  os: string;
  arch: string;
  agent_version: string;
  ipv4: string;
  ipv6: string;
  region: string;
  country_code: string;
  isp: string;
  notes: string;
  tags: number[];
  cpu: number | null;
  cores: number;
  mem_total: number;
  mem_used: number;
  swap_total: number;
  swap_used: number;
  disk: { device: string; total: number; used: number }[];
  rx_speed: number;
  tx_speed: number;
  tcp_connections: number;
  udp_connections: number;
  traffic_monthly: { month: string; rx_bytes: number; tx_bytes: number };
  uptime: number;
  process_count: number;
  ping: Record<string, number>;
  ping_loss: Record<string, number>;
  expires_at: number;
  price_amount: number;
  price_currency: string;
  price_cycle: string;
  traffic_quota_bytes: number;
  last_seen: number;
}

export interface Tag {
  id: number;
  name: string;
  color: string;
}

export interface SessionInfo {
  id: number;
  created_at: string;
  expires_at: string;
  ip: string;
  user_agent: string;
  current: boolean;
}

export interface RegisterCodeInfo {
  hash: string;
  created_at: number;
  expires_at: number;
  used: boolean;
  used_by_agent_id: number;
}

export interface PingTarget {
  target: string;
  name: string;
  region?: string;
  isp?: string;
  ip_version: number;
  protocol: string;
}

export interface PingResult {
  target: string; name: string; method: string; ip_version: number;
  avg_latency: number; min_latency: number; max_latency: number;
  jitter: number; loss: number; packets_sent: number; packets_recv: number;
}

export interface MetricPoint {
  timestamp: number;
  cpu: number;
  mem: number;
  disk: { device: string; total: number; used: number }[];
  rx: number;
  tx: number;
  ping: PingResult[];
}

export interface DailyPoint {
  date: string;
  cpu_avg: number; cpu_max: number; mem_avg: number; mem_max: number;
  rx: number; tx: number;
  ping: MetricPoint["ping"];
}

export interface ReportFrame {
  type: string; timestamp: number; hostname: string;
  data: {
    cpu: { usage: number | null; cores?: number };
    memory: { total: number; used: number };
    network: { rx_speed: number; tx_speed: number };
  };
}

export interface HistoryResponse {
  range: string;
  granularity: "3s" | "5m" | "1d";
  points_5m?: MetricPoint[];
  points_daily?: DailyPoint[];
  realtime?: ReportFrame[];
}

export interface PublicPayload {
  share_id: string;
  title: string;
  logo_url: string;
  footer_text: string;
  servers: {
    display_name: string; hostname: string; online: boolean; country_code: string;
    region: string; isp: string; cpu: number | null;
    mem_used: number; mem_total: number;
    disk: { device: string; total: number; used: number }[];
    uptime: number; ping: Record<string, number>; ping_loss: Record<string, number>;
  }[];
  services?: {
    id: number; name: string; type: string; up: boolean; latency_ms: number;
    uptime_45d: number; recent?: { ts: number; ok: boolean; latency_ms: number }[];
    daily?: { date: string; total: number; ok: number; up_ratio: number }[];
  }[];
}

export interface AlertRule {
  id?: number;
  name: string;
  metric: string;
  operator: string;
  threshold: number;
  duration: number;
  enabled: boolean;
  notify_channel_id: number | null;
}

export interface AlertHistoryItem {
  id: number;
  rule_id: number;
  agent_id: number;
  status: string;
  value: number | null;
  started_at: number;
  updated_at: number;
}

export interface NotifyChannel {
  id?: number;
  name: string;
  type: "webhook" | "telegram" | "smtp";
  config: Record<string, unknown>;
}

export interface ShareConfig {
  share_id: string;
  title: string;
  logo_url: string;
  footer_text: string;
  agent_ids: number[];
}
