# 安全策略

XProbe 以“只读、安全优先”为核心目标。如果你发现安全问题，请不要在公开 Issue 中披露细节，优先通过 GitHub Security Advisory 私密报告，或联系仓库所有者（GitHub: YCJE）。

报告时请尽量包含：影响版本、复现步骤或最小 POC、影响范围、可能的修复建议。

## 安全基线

- **纯只读架构**：Server → Agent 没有命令、配置下发或远程执行通道；Agent WebSocket 入站只接受心跳确认。
- **认证与凭证**：单管理员；Argon2id 密码哈希；不透明会话 Token、Agent Token、注册码均只存 SHA-256 哈希；TOTP 与恢复码可选启用。
- **传输安全**：公网入口强制 HTTPS；Agent 只允许 HTTPS/WSS，支持证书有效期校验与 SPKI Pinning；Cloudflare 入口使用受信代理 CIDR。
- **出站安全**：Webhook/Telegram/SMTP 统一做私网、回环、链路本地和云元数据地址过滤，DNS 预解析后直连，禁止重定向跟随，响应体限读。
- **权限最小化**：Agent 以非 root `probe` 用户运行，systemd 使用 `NoNewPrivileges`、`ProtectSystem=strict`、`RestrictAddressFamilies`、`MemoryMax` 等约束。
- **数据库与密钥**：数据目录只允许服务用户访问；TOTP secret 和通知渠道配置使用 AES-GCM 加密，密钥文件权限 0600。
- **浏览器侧**：严格 CSP（脚本仅同源）、HttpOnly + SameSite=Strict 会话 Cookie、CSRF Origin 校验、前端不使用 `dangerouslySetInnerHTML` 或第三方运行时 JS。
- **输入边界**：Agent 上报、注册、Ping 目标、告警规则、通知配置和安装脚本参数均有长度、范围、控制字符与协议校验。

## 支持范围

- 主分支与最新发布版本：接收并处理安全修复。
- 旧版本：不承诺持续安全补丁，建议升级到最新版本。
