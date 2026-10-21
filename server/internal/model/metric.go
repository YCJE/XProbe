package model

import "time"

// MetricRecord is one aggregated data point (default every 5 min, §5.3).
type MetricRecord struct {
	ID        int64     `json:"id"`
	AgentID   int64     `json:"agent_id"`
	Timestamp time.Time `json:"timestamp"`
	CPUUsage  float64   `json:"cpu_usage"`
	MemUsage  float64   `json:"mem_usage"`
	DiskUsage string    `json:"disk_usage"` // JSON keyed by mount point
	NetRx     float64   `json:"net_rx"`
	NetTx     float64   `json:"net_tx"`
	PingData  string    `json:"ping_data"` // JSON keyed by target
}
