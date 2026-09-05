package model

import "time"

// M3: 认证请求/响应与仪表盘推送载荷。

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type SetupRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type SessionInfo struct {
	ID        int64     `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	IP        string    `json:"ip"`
	UserAgent string    `json:"user_agent"`
	Current   bool      `json:"current"`
}

// DashboardServer 仪表盘实时条目(管理员视图, 全字段; 分享页走白名单子集)。
type DashboardServer struct {
	ID             int64              `json:"id"`
	Hostname       string             `json:"hostname"`
	DisplayName    string             `json:"display_name"`
	Online         bool               `json:"online"`
	OS             string             `json:"os"`
	Arch           string             `json:"arch"`
	AgentVersion   string             `json:"agent_version"`
	IPv4           string             `json:"ipv4"`
	IPv6           string             `json:"ipv6"`
	Region         string             `json:"region"`
	CountryCode    string             `json:"country_code"`
	ISP            string             `json:"isp"`
	Tags           []int64            `json:"tags"`
	CPU            *float64           `json:"cpu"` // 百分比, nil=首采样
	Cores          int                `json:"cores"`
	MemTotal       uint64             `json:"mem_total"`
	MemUsed        uint64             `json:"mem_used"`
	SwapTotal      uint64             `json:"swap_total"`
	SwapUsed       uint64             `json:"swap_used"`
	Disk           []DiskUsage        `json:"disk"`
	RxSpeed        uint64             `json:"rx_speed"`
	TxSpeed        uint64             `json:"tx_speed"`
	TCPConnections int                `json:"tcp_connections"`
	UDPConnections int                `json:"udp_connections"`
	TrafficMonthly TrafficMonthly     `json:"traffic_monthly"`
	Uptime         uint64             `json:"uptime"`
	ProcessCount   int                `json:"process_count"`
	Ping           map[string]float64 `json:"ping"` // 目标显示名 → 平均延迟 ms
	PingLoss       map[string]float64 `json:"ping_loss"`
	// 元数据(NodeGet 风格)
	ExpiresAt         int64    `json:"expires_at"`
	PriceAmount       float64  `json:"price_amount"`
	PriceCurrency     string   `json:"price_currency"`
	PriceCycle        string   `json:"price_cycle"`
	TrafficQuotaBytes int64    `json:"traffic_quota_bytes"`
	Notes             string `json:"notes"`
	GeoLat            *float64 `json:"geo_lat"`
	GeoLon            *float64 `json:"geo_lon"`
	LastSeen          int64    `json:"last_seen"`
}

// Tag 彩色标签。
type Tag struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

// UpdateMetaRequest 服务器元数据编辑(全部管理员设置, 设计文档 5.8)。
type UpdateMetaRequest struct {
	DisplayName       string   `json:"display_name"`
	Region            string   `json:"region"`
	CountryCode       string   `json:"country_code"`
	ISP               string   `json:"isp"`
	TagIDs            []int64  `json:"tag_ids"`
	ExpiresAt         int64    `json:"expires_at"`
	PriceAmount       float64  `json:"price_amount"`
	PriceCurrency     string   `json:"price_currency"`
	PriceCycle        string   `json:"price_cycle"`
	TrafficQuotaBytes int64    `json:"traffic_quota_bytes"`
	GeoLat            *float64 `json:"geo_lat"`
	GeoLon            *float64 `json:"geo_lon"`
}

// ResetFingerprintRequest 指纹重置(设计文档 7.5)。
type ResetFingerprintRequest struct {
	Note string `json:"note"` // 备注, 仅日志
}

// RegisterCodeInfo 注册码管理条目(哈希即标识, 展示用掩码)。
type RegisterCodeInfo struct {
	Hash          string `json:"hash"`
	CreatedAt     int64  `json:"created_at"`
	ExpiresAt     int64  `json:"expires_at"`
	Used          bool   `json:"used"`
	UsedByAgentID int64  `json:"used_by_agent_id"`
}

// ResetTokenResponse Token 重置响应(新 Token 仅此一次完整下发, S9)。
type ResetTokenResponse struct {
	Token string `json:"token"`
}

// MetricPoint 5 分钟聚合历史点(设计文档 5.3)。
type MetricPoint struct {
	Timestamp int64        `json:"timestamp"`
	CPU       float64      `json:"cpu"`
	Mem       float64      `json:"mem"`
	Disk      []DiskUsage  `json:"disk"`
	Rx        uint64       `json:"rx"`
	Tx        uint64       `json:"tx"`
	Ping      []PingResult `json:"ping"`
}

// DailyPoint 日聚合历史点(设计文档 5.6 metric_records_daily)。
type DailyPoint struct {
	Date   string       `json:"date"`
	CPUAvg float64      `json:"cpu_avg"`
	CPUMax float64      `json:"cpu_max"`
	MemAvg float64      `json:"mem_avg"`
	MemMax float64      `json:"mem_max"`
	Rx     uint64       `json:"rx"`
	Tx     uint64       `json:"tx"`
	Ping   []PingResult `json:"ping"`
}

// HistoryResponse 历史查询响应(时间范围 → 粒度, 设计文档 6.5)。
type HistoryResponse struct {
	Range       string        `json:"range"`
	Granularity string        `json:"granularity"` // 3s | 5m | 1d
	Points5m    []MetricPoint `json:"points_5m,omitempty"`
	PointsDaily []DailyPoint  `json:"points_daily,omitempty"`
	Realtime    []Report      `json:"realtime,omitempty"` // 1h/6h 环形缓冲原始帧
}

// CreateNodeRequest 预创建节点(Komari 模式): 先建节点拿安装命令, Agent 注册时绑定。
type CreateNodeRequest struct {
	Name  string `json:"name"`
	Notes string `json:"notes"`
}
