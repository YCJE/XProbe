package model

// M2 通信协议结构: REST 注册、WS 心跳、配置拉取(设计文档 4.2/4.7/5.2)。

// Heartbeat 心跳帧(Agent → Server)。
type Heartbeat struct {
	Type      FrameType `json:"type"` // "heartbeat"
	Timestamp int64     `json:"timestamp"`
}

// HeartbeatAck 心跳确认帧(Server → Agent, 协议中 Server→Agent 唯一帧, 设计文档 5.2)。
type HeartbeatAck struct {
	Type FrameType `json:"type"` // "heartbeat_ack"
}

// FrameHeartbeatAck 帧类型常量。
const FrameHeartbeatAck FrameType = "heartbeat_ack"

// RegisterRequest Agent 首次注册请求(POST /api/v1/agent/register, HTTPS REST)。
type RegisterRequest struct {
	RegisterCode    string `json:"register_code"`
	Hostname        string `json:"hostname"`
	HostFingerprint string `json:"host_fingerprint"` // SHA256(install_salt+CPU型号+主网卡MAC+GOOS), 设计文档 7.5
	OS              string `json:"os"`
	Arch            string `json:"arch"`
	AgentVersion    string `json:"agent_version"`
	// IPv4/IPv6 出口地址; 服务端会用请求 RemoteAddr 校正/回填 IPv4。
	IPv4 string `json:"ipv4"`
	IPv6 string `json:"ipv6"`
}

// RegisterResponse 注册成功响应; Token 仅此一次完整下发, 服务端只存哈希(S9)。
type RegisterResponse struct {
	Token   string `json:"token"`
	AgentID int64  `json:"agent_id"`
}

// PingTarget 探测目标(Agent 经 GET /api/v1/agent/config 拉取, 只读数据非控制指令)。
type PingTarget struct {
	Target    string `json:"target"`
	Name      string `json:"name"`
	Region    string `json:"region,omitempty"`
	ISP       string `json:"isp,omitempty"`
	IPVersion int    `json:"ip_version"`
	Protocol  string `json:"protocol"` // icmp / tcp(带端口自动判定)
}

// AgentConfigPayload 配置拉取响应载荷。
type AgentConfigPayload struct {
	PingTargets []PingTarget `json:"ping_targets"`
}
