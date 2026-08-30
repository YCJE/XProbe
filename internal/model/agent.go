package model

// Agent 是 Server 侧的 Agent 元数据(对应 agents 表, 设计文档 5.6)。
// Token 在库中仅存 SHA256 哈希(S9), 原文只在注册响应与 Agent 配置文件中出现。
type Agent struct {
	ID              int64
	TokenHash       string
	Hostname        string
	DisplayName     string
	OS              string
	Arch            string
	AgentVersion    string
	HostFingerprint string // 可为 NULL(reset-fingerprint 后待重新绑定)
	IPv4            string
	IPv6            string
	// 元数据(全部管理员设置, 设计文档 5.8)
	Region            string
	CountryCode       string
	ISP               string
	TagIDs            string // JSON 数组字符串
	ExpiresAt         int64
	PriceAmount       float64
	PriceCurrency     string
	PriceCycle        string
	TrafficQuotaBytes int64

	CreatedAt int64
	LastSeen  int64
	Online    bool
}
