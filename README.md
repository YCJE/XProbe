# XProbe

安全优先的纯只读服务器探针（监控）系统。从架构上杜绝 RCE 攻击面（Server→Agent 无任何控制通道），功能对标 Nezha/Komari，视觉对标 NodeGet。

> **当前阶段**：设计文档 v1.3 已定稿，开发尚未开始。完整设计规格与 AI 开发提示词见 [docs/server_probe_design_v1.3.md](docs/server_probe_design_v1.3.md)。

## 核心特性（规划）

- **纯只读架构**：WebSocket 协议中 Server→Agent 方向仅有 1 种心跳确认帧，不存在任何命令/配置下发帧（S1/S4，CI 自动验证 Agent 二进制零 `os/exec`）
- **强制 TLS + 证书 Pinning**：Agent 拒绝明文连接，支持自签/私有 CA 场景的指纹校验与平滑轮换
- **非 root 运行**：systemd 加固 + setcap 最小能力，ICMP 不可用时自动降级 TCP Ping
- **凭证哈希存储**：Agent Token / 注册码 / 会话在数据库仅存 SHA256 哈希
- **SSRF 防护**：Webhook/Telegram/SMTP 通知统一走内网过滤 + DNS 预解析 + 重定向检测 + 响应体限制
- **网络探测**：ICMP 10 包采样，报告延迟/最小/最大/抖动/丢包完整统计；IPv4/IPv6 双栈
- **NodeGet 风格仪表盘**：玻璃拟态卡片、SVG 圆环指标、延迟小格子图、卡片/表格双视图（地图视图 v2）、深浅双主题
- **告警 + 通知**：阈值状态机（防抖/静默/恢复通知），含月流量配额与 VPS 到期提醒
- **单二进制部署**：前端 embed + Agent 二进制内嵌分发，一键安装完全自包含

## 安全原则

S1 纯只读 · S2 强制 TLS · S3 非 root · S4 无远程执行 · S5 单管理员强认证 · S6 SSRF 防护 · S7 最小权限采集 · S8 配置权限 600 · S9 凭证哈希存储

## 技术栈

Go 1.22+ · Gin · gorilla/websocket · SQLite (WAL) · React 18 + TypeScript · Vite · shadcn/ui · Tailwind CSS · ECharts · pro-bing

## 文档

- [设计文档 v1.3](docs/server_probe_design_v1.3.md) —— 产品规格、架构、安全设计、部署运维、里程碑（M0-M6）与 AI 开发提示词

## License

待定（TBD）
