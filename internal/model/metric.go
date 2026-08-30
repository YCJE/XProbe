// Package model 定义 Agent 与 Server 共享的数据结构。
// JSON 字段与设计文档 4.4/4.5 节的上报格式一一对应, 修改需同步文档。
package model

import "time"

// FrameType 上报帧类型(设计文档 5.2: 仅 report/ping_result/heartbeat 三种入站帧)。
type FrameType string

const (
	FrameReport     FrameType = "report"
	FramePingResult FrameType = "ping_result"
	FrameHeartbeat  FrameType = "heartbeat"
)

// Token 不在帧内——WS 握手时经 Authorization 头校验(设计文档 5.2), 帧内禁止携带。
type Report struct {
	Type      FrameType  `json:"type"`
	Timestamp int64      `json:"timestamp"`
	Hostname  string     `json:"hostname"`
	Data      ReportData `json:"data"`
}

type CPUInfo struct {
	// Usage 为两次 /proc/stat 采样差值的使用率(0-100)。
	// Agent 启动后首个采样周期置 nil(前端显示 --), 避免单采样产生假值(设计文档 4.1)。
	Usage  *float64 `json:"usage"`
	Cores  int      `json:"cores"`
	Model  string   `json:"model"`
	Load1  float64  `json:"load_1"`
	Load5  float64  `json:"load_5"`
	Load15 float64  `json:"load_15"`
}

type MemoryInfo struct {
	Total     uint64 `json:"total"`
	Used      uint64 `json:"used"`
	SwapTotal uint64 `json:"swap_total"`
	SwapUsed  uint64 `json:"swap_used"`
}

type DiskUsage struct {
	// Device 存挂载点(与设计文档 4.4 示例一致, 如 "/"、"/data")。
	Device string `json:"device"`
	Total  uint64 `json:"total"`
	Used   uint64 `json:"used"`
}

type NetworkInfo struct {
	RxSpeed        uint64 `json:"rx_speed"`
	TxSpeed        uint64 `json:"tx_speed"`
	TCPConnections int    `json:"tcp_connections"`
	UDPConnections int    `json:"udp_connections"`
}

type TrafficMonthly struct {
	// Month 为 UTC 自然月标记 "2026-08"(设计文档 4.4: 月界统一 UTC)。
	Month   string `json:"month"`
	RxBytes uint64 `json:"rx_bytes"`
	TxBytes uint64 `json:"tx_bytes"`
}

type IPInfo struct {
	IPv4 string `json:"ipv4"`
	IPv6 string `json:"ipv6"`
}

// SystemInfo 系统静态信息, 注册时随主机指纹一并上报并写入 agents 表。
type SystemInfo struct {
	OS           string `json:"os"` // /etc/os-release PRETTY_NAME
	Kernel       string `json:"kernel"`
	Arch         string `json:"arch"`
	AgentVersion string `json:"agent_version"`
}

type ReportData struct {
	CPU            CPUInfo        `json:"cpu"`
	Memory         MemoryInfo     `json:"memory"`
	Disk           []DiskUsage    `json:"disk"`
	Network        NetworkInfo    `json:"network"`
	TrafficMonthly TrafficMonthly `json:"traffic_monthly"`
	IPInfo         IPInfo         `json:"ip_info"`
	System         SystemInfo     `json:"system"`
	Uptime         uint64         `json:"uptime"`
	ProcessCount   int            `json:"process_count"`
}

// PingResult 单个探测目标的完整统计(设计文档 4.5)。
type PingResult struct {
	Target      string  `json:"target"`
	Name        string  `json:"name"`
	Method      string  `json:"method"` // icmp / icmp_unprivileged / tcp / http
	IPVersion   int     `json:"ip_version"`
	AvgLatency  float64 `json:"avg_latency"`
	MinLatency  float64 `json:"min_latency"`
	MaxLatency  float64 `json:"max_latency"`
	Jitter      float64 `json:"jitter"`
	Loss        float64 `json:"loss"` // 百分比 0-100
	PacketsSent int     `json:"packets_sent"`
	PacketsRecv int     `json:"packets_recv"`
}

// PingReport ping_result 帧。
type PingReport struct {
	Type FrameType    `json:"type"`
	Data []PingResult `json:"data"`
}

// MonthOf 返回 t 所在的 UTC 自然月标记(设计文档 4.4: 月界统一 UTC)。
func MonthOf(t time.Time) string {
	return t.UTC().Format("2006-01")
}
