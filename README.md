# XProbe

一个安全优先的**只读**服务器探针。Agent 只采集和上报，主控不提供 WebSSH、文件管理、远程命令或定时任务。目标是避免把监控面板变成服务器管理入口。

## 特性

- 服务器状态：CPU、内存、磁盘、网络速度、月流量、负载、运行时间、进程/连接数
- 延迟探测：ICMP（优先无特权）、TCP、HTTP HEAD；延迟格子与历史曲线
- 实时面板：React + 自定义 CSS/SVG，卡片/表格视图，服务器详情
- 7 套内置 CSS 主题：新版「任务控制 / 纸面编辑 / 石墨琥珀」+ 经典「玻璃拟态 / 经典实底 / 极简描边 / 深色高对比」，均支持明/暗/跟随系统
- 告警：CPU/内存/磁盘/离线规则，Webhook / Telegram / SMTP，SSRF 过滤
- 安全：Agent Token/注册码/会话 Token 全部哈希存储；Argon2id；TOTP；审计日志
- 部署：单二进制、嵌入式前端、Docker、systemd；Cloudflare Tunnel / 橙云入口
- 公开状态页：随机分享 ID，白名单字段，不展示 IP/Token/配置

## 快速开始（本机）

要求：Go 1.26.9（go.mod 中的 toolchain 会自动下载补丁版）、Node.js 24。

```bash
# Linux / macOS / Git Bash（有 make 时）
make build
# 或者不依赖 make：
./scripts/build.sh
# Windows PowerShell：
# .\scripts\build.ps1

./bin/probe-server
```

默认监听 `127.0.0.1:8080`，数据在 `./data`。首次启动会在日志中打印 setup token，同时写入 `data/setup_token`（0600）。打开 `http://127.0.0.1:8080/setup` 完成初始化，然后用管理员账号登录。

> `go build` 需要先构建前端，因为前端产物通过 `go:embed` 编入二进制。使用 `make build` 或先执行 `make web`。

## 添加节点

1. 登录面板，点击“添加节点”，生成一次性注册码。
2. 把对应架构的 Agent 二进制放到 `data/agent-dist/{amd64,arm64}/`：
   - `probe-agent`
   - `probe-agent.sha256`
   - `probe-agent.sig`（可选，Ed25519 签名）
3. 在目标 Linux 机器上运行：

```bash
curl -fsSL https://monitor.example.com/install-agent.sh | sudo sh -s -- \
  --server https://monitor.example.com \
  --code XXXX-XXXX-XXXX-XXXX-XXXX-XXXX-XX
```

安装脚本会创建 `probe` 用户、安装 systemd 单元、设置非特权 ICMP、启动 Agent。Agent 首次注册后会把 Token 写回 `/etc/probe-agent/config.yml`（0600），并删除注册码。

如果暂时没有 server 托管的 Agent 包，也可以手动复制二进制并写配置，格式见 `agent/config.example.yaml`。

## 配置

服务端示例见 `server/config.example.yaml`，完整字段说明在设计文档 §8.2。

常用安全基线：

- 默认只监听回环；公网部署必须 `tls.mode=files`，或使用 Cloudflare Tunnel。
- `tls.mode=off` 只允许回环监听；Cloudflare Tunnel 模式下 Agent 仍连接 `wss://` 公网域名。
- `proxy.mode=cloudflare_tunnel` 只信任回环传来的 `CF-Connecting-IP` / `CF-Visitor`。
- 不要把 `data/`、证书、`config.yaml` 提交到仓库；`.gitignore` 已覆盖常见文件。

## Cloudflare

### Tunnel（推荐）

```bash
cd deploy
# 编辑 config.tunnel.yaml 的 public_url
CLOUDFLARE_TUNNEL_TOKEN=... docker compose -f compose.tunnel.yaml up -d --build
```

`cloudflared` 与 probe 共享网络命名空间，直接访问 `127.0.0.1:8080`，不需要开放公网端口。管理端可以再挂 Cloudflare Access；Agent 的 `/api/v1/agent/*` 和 `/download/*`、`/install-agent.sh` 需要 bypass。

容器以 UID `10001` 运行，绑定挂载前先准备数据目录：

```bash
mkdir -p data && sudo chown -R 10001:10001 data
```

### 橙云 + Origin Certificate

使用 `deploy/compose.tls.yaml`，源站提供 443/8443 HTTPS，Cloudflare SSL 设为 Full (strict)，并开启 Authenticated Origin Pulls、只允许 Cloudflare IP 访问源站。`proxy.trusted_cidrs` 填入 Cloudflare IP 段。证书和私钥必须对容器 UID `10001` 可读（例如 `chown 10001:10001 certs/* && chmod 0600 certs/key.pem`）。

不支持把 Go 服务端直接跑在 Workers 上；Workers 版本需要独立的 TypeScript + D1/Durable Objects 实现。

## 发布与签名

```bash
# 离线保存私钥；只在发布机运行
PROBE_RELEASE_KEY=/secure/probe-release.pem ./deploy/release-sign.sh \
  data/agent-dist/amd64/probe-agent \
  data/agent-dist/arm64/probe-agent
```

安装脚本会用 `PROBE_RELEASE_PUBKEY` 指定的公钥验签（需要 `openssl`）；未提供公钥时只校验 SHA-256 并给出警告。

## 安全边界

- 协议中没有命令帧；Server 不主动连接 Agent；Agent 只接受 `heartbeat_ack`。
- Agent 以非 root `probe` 用户运行，systemd 使用 `NoNewPrivileges`、`ProtectSystem=strict`、`RestrictAddressFamilies`、`MemoryMax` 等。
- Agent Token、注册码、管理会话 Token 在数据库中只存 SHA-256。
- 管理端使用不透明会话，不使用 JWT，也没有 `jwt_secret`。
- 主题为编译进前端的 CSS 主题，不支持第三方 JS/主题市场。
- Webhook/SMTP 出站经过私网/回环/链路本地地址过滤，禁止重定向，响应体限 1 KiB。

详见 `docs/server_probe_design.md`（v1.5 实现基线）。

## 开发

```bash
make test
make vet
make web    # 单独构建前端
```

目录：

- `server/` — Go 主控、API、WebSocket、嵌入的前端
- `agent/` — Linux Agent、采集、Ping、WS 上报
- `common/` — 共享 wire 协议
- `deploy/` — Docker Compose、安装脚本、发布签名
- `server/web/` — React + Vite 前端源码
- `docs/` — 设计文档

Agent 采集器目前仅支持 Linux；Windows/macOS 仅能编译，不能实际采集。
