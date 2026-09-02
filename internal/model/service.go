package model

// 服务监控拨测(Nezha 对标): Server 主动对服务端点探活, 与 Agent 通道无关(不违反 S1)。

type Service struct {
	ID              int64  `json:"id"`
	Name            string `json:"name"`
	Type            string `json:"type"` // http / tcp / icmp
	Target          string `json:"target"`
	Port            int    `json:"port"`
	Path            string `json:"path"`
	IntervalSec     int    `json:"interval_sec"`
	Enabled         bool   `json:"enabled"`
	NotifyChannelID *int64 `json:"notify_channel_id"`
	CreatedAt       int64  `json:"created_at"`
}

type ServiceDaily struct {
	Date    string  `json:"date"`
	Total   int64   `json:"total"`
	Ok      int64   `json:"ok"`
	UpRatio float64 `json:"up_ratio"` // 0-100
}

// ServiceResult 单次探测结果(内存环形, 状态页最近 N 次条)。
type ServiceResult struct {
	Ts        int64   `json:"ts"`
	OK        bool    `json:"ok"`
	LatencyMs float64 `json:"latency_ms"`
}

// ServiceStatus 面板/状态页的服务条目(白名单: 不含 target 内部地址)。
type ServiceStatus struct {
	ID       int64           `json:"id"`
	Name     string          `json:"name"`
	Type     string          `json:"type"`
	Up       bool            `json:"up"`
	Latency  float64         `json:"latency_ms"`
	Uptime45 float64         `json:"uptime_45d"` // 0-100, 近 45 天加权
	Recent   []ServiceResult `json:"recent"`     // 最近 N 次结果
	Daily    []ServiceDaily  `json:"daily"`      // 近 45 天日汇总(旧→新)
}

// TrafficReportRow 报表: 月度流量行(设计文档 6.6 报表页)。
type TrafficReportRow struct {
	AgentID int64  `json:"agent_id"`
	Name    string `json:"name"`
	Month   string `json:"month"`
	Rx      uint64 `json:"rx"`
	Tx      uint64 `json:"tx"`
}
