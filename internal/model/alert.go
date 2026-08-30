package model

// M5: 告警规则、告警历史、通知渠道、分享页(设计文档 5.4/5.5/6.6)。

type AlertRule struct {
	ID              int64   `json:"id"`
	Name            string  `json:"name"`
	Metric          string  `json:"metric"`   // cpu_usage/mem_usage/disk_usage/agent_offline/traffic_quota/expire_days
	Operator        string  `json:"operator"` // > / < / =
	Threshold       float64 `json:"threshold"`
	Duration        int64   `json:"duration"` // 秒, 防抖
	Enabled         bool    `json:"enabled"`
	NotifyChannelID *int64  `json:"notify_channel_id"`
}

// 指标语义(设计文档 10.7): traffic_quota 传已用百分比(通常 >),
// expire_days 传剩余天数(通常 <)。
const (
	MetricCPU         = "cpu_usage"
	MetricMem         = "mem_usage"
	MetricDisk        = "disk_usage"
	MetricOffline     = "agent_offline"
	MetricTrafficQuot = "traffic_quota"
	MetricExpireDays  = "expire_days"
)

type AlertHistory struct {
	ID        int64    `json:"id"`
	RuleID    int64    `json:"rule_id"`
	AgentID   int64    `json:"agent_id"`
	Status    string   `json:"status"` // PENDING/FIRING/RESOLVED
	Value     *float64 `json:"value"`
	StartedAt int64    `json:"started_at"`
	UpdatedAt int64    `json:"updated_at"`
}

type NotifyChannel struct {
	ID     int64          `json:"id"`
	Name   string         `json:"name"`
	Type   string         `json:"type"` // webhook/telegram/smtp
	Config map[string]any `json:"config"`
}

// SharePageConfig 公开分享页(设计文档 6.6)。
type SharePageConfig struct {
	ShareID    string  `json:"share_id"`
	Title      string  `json:"title"`
	LogoURL    string  `json:"logo_url"`
	FooterText string  `json:"footer_text"`
	AgentIDs   []int64 `json:"agent_ids"`
}

// PublicSharePayload 免登录状态页载荷(白名单字段, 无 IP/Token/配置, T11)。
type PublicSharePayload struct {
	ShareID    string                `json:"share_id"`
	Title      string                `json:"title"`
	LogoURL    string                `json:"logo_url"`
	FooterText string                `json:"footer_text"`
	Servers    []PublicShareServer   `json:"servers"`
}

type PublicShareServer struct {
	DisplayName string             `json:"display_name"`
	Hostname    string             `json:"hostname"`
	Online      bool               `json:"online"`
	CountryCode string             `json:"country_code"`
	Region      string             `json:"region"`
	ISP         string             `json:"isp"`
	CPU         *float64           `json:"cpu"`
	MemUsed     uint64             `json:"mem_used"`
	MemTotal    uint64             `json:"mem_total"`
	Disk        []DiskUsage        `json:"disk"`
	Uptime      uint64             `json:"uptime"`
	Ping        map[string]float64 `json:"ping"`
	PingLoss    map[string]float64 `json:"ping_loss"`
}
