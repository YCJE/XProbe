# 安全策略

XProbe 以安全为核心卖点。如发现安全漏洞,请**不要**在公开 Issue 中披露。

## 报告流程

1. 发送邮件至仓库所有者(GitHub 账户 YCJE 的个人邮箱),或通过 GitHub Security Advisory 私密报告。
2. 请包含:漏洞描述、复现步骤/POC、影响评估。
3. 我们承诺 72 小时内确认收到,7 天内给出初步评估。

## 安全设计基线(详见 docs/server_probe_design_v1.3.md)

| 原则 | 说明 |
|------|------|
| S1 纯只读架构 | Server→Agent 无任何控制通道;WS 协议仅 4 种帧,下行只有 heartbeat_ack |
| S2 强制 TLS | 全链路加密;证书指纹 Pinning;最低 TLS 1.2 |
| S3 非 root | Agent 以 probe 用户运行,systemd 加固,最小 capabilities |
| S4 无远程执行 | Agent 代码零 `os/exec`;CI `audit-noexec` 门禁强制零匹配 |
| S5 单管理员 | 强密码策略 + 登录限速锁定 + JWT 会话吊销 |
| S6 SSRF 防护 | 通知外呼:内网过滤/DNS 预解析/重定向校验/响应体限读 |
| S7 最小权限采集 | 只读 /proc 与系统调用,不读其他用户进程 |
| S8 配置权限 600 | Agent 配置与状态文件仅属主可读写 |
| S9 凭证哈希存储 | Token/注册码/会话在数据库仅存 SHA256 |

## 支持版本

- 最新 release 主版本:支持安全补丁
- 旧版本:不承诺
