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
	HostFingerprint string
	IPv4            string
	IPv6            string
	CreatedAt       int64
	LastSeen        int64
	Online          bool
}
