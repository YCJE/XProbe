// Package wire defines the Agent↔Server message frames and payload types.
// It is shared by both binaries and is the single source of truth for the
// on-the-wire format (design doc §4.4, §4.5, §5.2).
package wire

// Frame types (§5.2). Agent→Server accepts exactly these three; Server→Agent
// sends exactly one. There is deliberately no command/config frame.
const (
	TypeReport       = "report"
	TypePingResult   = "ping_result"
	TypeHeartbeat    = "heartbeat"
	TypeHeartbeatAck = "heartbeat_ack"
)

// Limits enforced by server-side validation (§7.6).
const (
	MaxHostnameLen    = 128
	MaxDiskEntries    = 32
	MaxPingEntries    = 64
	MaxLatencyMs      = 60000
	MaxNetSpeedBytes  = 1e12 // 1 TB/s, far above any real NIC
	MaxDiskDeviceLen  = 512
	MaxFrameSizeBytes = 10 * 1024
)

// Report is the periodic metrics frame (default every 3s). Token is never
// carried inside frames; it is validated via the Authorization header during
// the WebSocket handshake.
type Report struct {
	Type         string     `json:"type"`
	Timestamp    int64      `json:"timestamp"`
	Hostname     string     `json:"hostname"`
	AgentVersion string     `json:"agent_version"`
	Data         ReportData `json:"data"`
}

// ReportData groups all collected metrics for one reporting interval.
type ReportData struct {
	CPU          CPUMetrics     `json:"cpu"`
	Memory       MemoryMetrics  `json:"memory"`
	Disk         []DiskMetrics  `json:"disk"`
	Network      NetworkMetrics `json:"network"`
	TrafficMonth TrafficMonthly `json:"traffic_monthly"`
	IPInfo       IPInfo         `json:"ip_info"`
	Uptime       int64          `json:"uptime"`
	ProcessCount int            `json:"process_count"`
}

type CPUMetrics struct {
	Usage  float64 `json:"usage"`
	Cores  int     `json:"cores"`
	Model  string  `json:"model"`
	Load1  float64 `json:"load_1"`
	Load5  float64 `json:"load_5"`
	Load15 float64 `json:"load_15"`
}

type MemoryMetrics struct {
	Total     uint64 `json:"total"`
	Used      uint64 `json:"used"`
	SwapTotal uint64 `json:"swap_total"`
	SwapUsed  uint64 `json:"swap_used"`
}

type DiskMetrics struct {
	Device string `json:"device"`
	Total  uint64 `json:"total"`
	Used   uint64 `json:"used"`
}

type NetworkMetrics struct {
	RxSpeed        float64 `json:"rx_speed"`
	TxSpeed        float64 `json:"tx_speed"`
	TCPConnections int     `json:"tcp_connections"`
	UDPConnections int     `json:"udp_connections"`
}

// TrafficMonthly carries the agent-side cumulative byte counters for the
// current calendar month (agent-local timezone, §4.4).
type TrafficMonthly struct {
	Month   string `json:"month"` // "2026-08", agent-local timezone
	RxBytes uint64 `json:"rx_bytes"`
	TxBytes uint64 `json:"tx_bytes"`
}

type IPInfo struct {
	IPv4 string `json:"ipv4"`
	IPv6 string `json:"ipv6"`
}

// PingResult carries one probe round for all targets (§4.5).
type PingResult struct {
	Type      string     `json:"type"`
	Timestamp int64      `json:"timestamp"`
	Data      []PingStat `json:"data"`
}

type PingStat struct {
	Target      string  `json:"target"`
	Name        string  `json:"name"`
	Method      string  `json:"method"` // icmp / icmp_unprivileged / tcp / http
	IPVersion   int     `json:"ip_version"`
	AvgLatency  float64 `json:"avg_latency"`
	MinLatency  float64 `json:"min_latency"`
	MaxLatency  float64 `json:"max_latency"`
	Jitter      float64 `json:"jitter"`
	Loss        float64 `json:"loss"` // percent 0-100
	PacketsSent int     `json:"packets_sent"`
	PacketsRecv int     `json:"packets_recv"`
}

// Heartbeat is sent every 30s to keep liveness state; the server replies with
// HeartbeatAck (the only Server→Agent frame that exists).
type Heartbeat struct {
	Type      string `json:"type"`
	Timestamp int64  `json:"timestamp"`
}

type HeartbeatAck struct {
	Type string `json:"type"`
}

// RegisterRequest is the one-time REST body sent by an agent holding a
// registration code. The code itself travels in the Authorization header and
// is never part of this JSON.
type RegisterRequest struct {
	Hostname        string `json:"hostname"`
	OS              string `json:"os"`
	Arch            string `json:"arch"`
	AgentVersion    string `json:"agent_version"`
	HostFingerprint string `json:"host_fingerprint"`
	InstallSalt     string `json:"install_salt"`
}

// RegisterResponse carries the only plaintext copy of the agent token.
type RegisterResponse struct {
	AgentID int64  `json:"agent_id"`
	Token   string `json:"token"`
}

// AgentConfig is the read-only configuration an agent pulls over HTTPS. It
// contains no command or free-form shell fields (S1/S9).
type AgentConfig struct {
	ReportInterval     int          `json:"report_interval"`
	ConfigSyncInterval int          `json:"config_sync_interval"`
	PingInterval       int          `json:"ping_interval"`
	PingTargets        []PingTarget `json:"ping_targets"`
}

type PingTarget struct {
	ID        int64  `json:"id"`
	Target    string `json:"target"`
	Name      string `json:"name"`
	Region    string `json:"region,omitempty"`
	ISP       string `json:"isp,omitempty"`
	IPVersion int    `json:"ip_version"`
	Protocol  string `json:"protocol"`
	Enabled   bool   `json:"enabled"`
}
