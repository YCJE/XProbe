# 服务器探针开发计划与设计文档

> **文档用途**：本文档既是产品设计规格，也是可直接用于 AI 编程工具（如 TRAE、Cursor、Claude）的开发提示词。文档包含完整的技术规格、架构设计、安全要求、开发里程碑，以及每个设计决策的原因解释。
>
> **生成日期**：2026-06-21
> **更新日期**：2026-10-20
> **版本**：v1.5（v1.5 变更：M3–M6 实现完成——React 19 + 自定义 CSS/SVG 面板、7 套内置主题（3 套新版 + 4 套经典）、浏览器实时 WS、Ping/历史数据、告警引擎、Webhook/Telegram/SMTP、TOTP、审计、公开页、Docker/systemd/安装脚本、Ed25519 发布签名；前端不引入 shadcn/Tailwind/ECharts 以缩小依赖面；同步 API/表结构与实现状态）
>
> v1.4 变更：代码仓库迁移到 `XProbe`；Go 1.26 + `database/sql`/`modernc.org/sqlite`（禁用 GORM AutoMigrate）；Agent Token、注册码、管理会话令牌一律哈希存储；管理端改为不透明会话并移除 JWT/`jwt_secret`；仅保留内置 CSS 主题，移除第三方主题上传/市场/运行时 JS；新增 Cloudflare Tunnel/橙云两种入口与受信代理模型；Agent 二进制改为 Ed25519 验签；证书 Pinning 仅用于直连自签场景；同步 M2/M3 任务与 §10 提示词）
>
> v1.3 变更：新增 S9 受限探测通道原则与 T13 威胁场景；修复 systemd NoNewPrivileges 与 setcap 互斥导致的 ICMP 静默降级，改用 AmbientCapabilities；补齐 unprivileged ICMP 的 ping_group_range 前置条件；告警规则新增作用域 scope 与多渠道 notify_channel_ids；补齐 sessions 表与 agents.geo_lat/geo_lon 列；公开分享页新增实时 WS 通道；注册端点限速；新增安全响应头 CSP/HSTS；新增首次初始化 setup token 流程与 TOTP 恢复码；JWT 统一 2h+静默续期；HTTP Ping 收紧与探测并发调度；月流量时区规则；state.json 写盘节流；备份改用 SQLite backup API；明确内嵌 migration 工具；同步第 9/10 章里程碑任务与提示词）
>
> v1.2 变更：全文档一致性审查修订——第 10 章提示词与设计主体同步；注册统一为 REST 通道，删除 Server→Agent 推送帧；新增 Agent 证书 Pinning 防 MITM；Agent Token 改 Header 传输；JWT 缩短有效期并支持吊销；SSRF 防护扩展至 SMTP；补齐 alert_history/sessions 表与删除/指纹重置等缺失 API；修正环形缓冲计算与聚合规则；修复安装脚本目录属主；GPU 采集改 NVML 库绑定；明确 v1 仅支持 Linux）
>
> v1.1 变更：前端 UI 全面改版对标 NodeGet 风格；新增标签分组、多视图仪表盘、延迟格子图、地图视图、IPv4/IPv6 双栈、流量统计、VPS 到期/成本管理等功能

---

## 目录

1. [项目概述](#1-项目概述)
2. [核心安全原则](#2-核心安全原则)
3. [系统架构](#3-系统架构)
4. [Agent 详细设计](#4-agent-详细设计)
5. [Server 详细设计](#5-server-详细设计)
6. [前端面板设计](#6-前端面板设计)
7. [安全设计](#7-安全设计)
8. [部署与运维](#8-部署与运维)
9. [开发计划与里程碑](#9-开发计划与里程碑)
10. [AI 开发提示词](#10-ai-开发提示词)

---

## 1. 项目概述

### 1.1 项目背景

本项目旨在开发一款**以安全为核心**的服务器探针（监控）系统。起因是 2026 年 5 月主流探针 Nezha（哪吒探针）集中爆发了 9 个安全漏洞（含 2 个严重级别的跨租户 RCE），导致大量部署 Nezha 的服务器被入侵。

### 1.2 Nezha 漏洞根因分析

Nezha 的 9 个漏洞根本原因是：

| 根因 | 说明 | 对应漏洞 |
|------|------|---------|
| 多租户授权缺失 | 引入多用户功能时，大量 API 路由使用 `commonHandler`（仅需认证）而非 `adminHandler`（需管理员权限），且关键操作缺失对象级所有权检查（BOLA/IDOR） | CVE-2026-46716 等 9 个漏洞 |
| 默认明文通信 | Agent 与 Dashboard 之间 gRPC 通道默认不加密（`tls: false`），同网段攻击者可截获凭证并注入命令 | NEZHA-AGENT-001 |
| 控制通道攻击面大 | 远程命令执行、WebSSH 终端、文件管理器、Cron 任务分发等多个路径都成为 RCE 入口 | CVE-2026-46716、GHSA-q6xx-5vr8-p898 |
| Webhook SSRF | 通知 Webhook 无内网地址过滤，且无限制反射完整响应体 | GHSA-w4g9-mxgg-j532 |
| Agent 默认 root 运行 | RCE 发生时即为 root 权限，危害被放大 | 所有 RCE 漏洞 |

### 1.3 产品定位

| 维度 | 定位 |
|------|------|
| **核心卖点** | 安全第一的纯只读服务器监控探针 |
| **差异化** | 从架构上彻底消除 RCE 攻击面（无控制通道），区别于 Nezha/Komari |
| **目标用户** | 个人开发者、小团队（第一版聚焦个人自用几台到十几台，架构预留扩展空间） |
| **参考产品** | Nezha（功能参考）、Komari（轻量参考）、NodeGet（UI/UX 参考） |

### 1.3.1 NodeGet UI 借鉴要点

NodeGet（NodeSeekDev 出品的 Rust 探针）及其社区主题是当前 VPS 圈公认的高颜值代表。本探针前端对标其视觉风格：

| NodeGet UI 特征 | 本探针采纳方式 |
|-----------------|---------------|
| CPU/内存/磁盘**圆环**进度展示 | 卡片内三个小圆环，替代传统进度条 |
| **延迟小格子**图（延迟越高格子越高、颜色柔和渐变） | 卡片与详情页核心组件 |
| **卡片/表格/地图三视图** | 仪表盘支持三视图切换 |
| 标签筛选、地区筛选、搜索排序 | 仪表盘顶部筛选栏 |
| IPv4 / IPv6 Ping 分开展示 | 双栈延迟独立展示 |
| 玻璃拟态（Glassmorphism）卡片 + 可切换背景 | 毛玻璃卡片 + 背景（纯色/渐变/自定义） |
| 浅色/深色模式 + 柔和配色 | 主题系统支持 |
| VPS 到期时间 / 剩余价值展示 | 服务器元数据（到期/成本/币种）+ 到期提醒告警 |
| 移动端适配 | 响应式布局 |

**注意**：NodeGet 的一些功能**不采纳**，因为与我们的安全红线冲突：
- Js Worker 插件系统（服务端执行任意 JS，违反 S4 无远程执行）
- 完全前后端分离的静态部署（增加部署复杂度，与单二进制定位冲突）
- 细粒度多用户权限（违反 S5 单管理员原则）
- Server 主动 Ping/IP 查询功能（我们是 Agent 本地探测后上报，不由 Server 发起）

### 1.4 核心决策汇总

以下是通过需求分析确定的核心设计决策：

| 决策项 | 选择 | 理由 |
|--------|------|------|
| 安全边界 | 纯只读监控（A） | 从架构上杜绝 RCE 类漏洞，成为核心差异化卖点 |
| 使用规模 | 先个人自用，预留扩展（D） | 第一版聚焦安全只读，不背多租户复杂度包袱 |
| 网络探测 | 预置目标 + 自定义目标（D） | 开箱即用 + 灵活性，配置同步是只读数据不违反安全原则 |
| Agent 权限 | 非 root，自动选择探测方式（D） | 始终非 root，优先 setcap ICMP，降级 TCP Ping |
| 技术栈 | 全 Go + 前端内嵌（B） | 单二进制部署，Go 交叉编译多平台，前端 embed 极简部署 |
| 通信协议 | WebSocket + 强制 wss（B） | 强制 TLS 是底线，复用 HTTPS 证书生态，JSON 易调试 |
| 数据存储 | SQLite + 内存环形缓冲（D） | 零配置，内存缓冲吸收高频写入，聚合后落盘 |
| 告警通知 | 基础告警 + SSRF 防护 + 去重（C） | 对症防范 Nezha SSRF 漏洞，避免告警风暴 |
| 前端面板 | 标准面板 + 主题/自定义/分享（C） | 覆盖核心场景 + 个性化体验 |
| 前端 UI 风格 | 对标 NodeGet（圆环卡片/延迟格子/三视图/玻璃拟态） | VPS 圈公认高颜值风格，社区验证过的审美 |
| 部署支持 | 全平台多架构（D） | Go 交叉编译成本低，覆盖 VPS/NAS/树莓派/PC |
| 架构方案 | 经典分层架构（方案一） | 清晰分层适合 AI 工具按层开发，扩展性恰到好处 |

### 1.5 当前实现基线（优先于正文旧描述）

| 决策项 | 基线 |
|---|---|
| 语言/存储 | Go 1.26.9；`database/sql` + `modernc.org/sqlite`；内嵌顺序迁移；不使用 GORM/AutoMigrate |
| Agent 凭据 | 每节点 256-bit Token；服务端只存 SHA-256；创建/重置时只显示一次 |
| 管理会话 | 不透明随机令牌；服务端存 SHA-256；Cookie HttpOnly + Secure + SameSite=Strict；2h 空闲/7d 绝对过期；无 JWT、无 `jwt_secret` |
| 注册码 | 服务端只存哈希；一次性；15 分钟；按 IP 限速；每窗口数量上限 |
| 主题 | 仅内置 7 套 CSS 变量主题（3 套新版 + 4 套经典）；不支持上传、市场、第三方 JS；背景仅内置渐变/本地图片 |
| 部署 | 自部署单二进制；可选 Cloudflare Tunnel（推荐）或橙云 + Origin Certificate；不支持 Workers 运行时 |
| TLS | 公网路径强制 TLS；仅允许回环地址为受信隧道提供明文；Agent 始终连接 `wss://` 公网域名 |
| 证书 Pinning | 仅直连自签场景启用；指纹由一键命令带外传入；Cloudflare 模式使用系统 CA，不 Pin 叶子证书 |
| Agent 分发 | 发布二进制 Ed25519 签名；安装/升级前验签；SHA-256 仅作完整性辅助 |
| Agent 权限 | 非 root 专用用户 + systemd 沙箱；ICMP 通过 AmbientCapabilities 或降级 TCP |

### 1.6 实现状态（2026-10-20）

| 里程碑 | 状态 | 说明 |
|---|---|---|
| M1 基础架构 + 采集 | 完成 | 采集器、SQLite 顺序迁移、ring buffer |
| M2 通信 + 注册 | 完成 | 配置层、TLS/CF 入口、setup/会话、注册码/Token、Agent WS、配置拉取 |
| M3 前端 + 主题 | 完成 | React 19 + 自定义 CSS/SVG；7 套内置主题（3 套新版 + 4 套经典）；卡片/表格/详情；浏览器 WS |
| M4 Ping + 历史 | 完成 | 无特权 ICMP / TCP / HTTP HEAD；5 分钟指标桶；ping_records；历史 API |
| M5 告警/通知/安全 | 完成 | 告警状态机、Webhook/Telegram/SMTP、SSRF 过滤、TOTP、审计、公开页 |
| M6 部署/发布 | 完成 | Dockerfile/compose、systemd 安装脚本、Ed25519 发布签名 |

已知边界：Agent 采集器仅 Linux；Docker/Cloudflare 真实环境需按 README 验证；第三方主题/远程管理不在范围内。

---

## 2. 核心安全原则

这一节定义了整个系统的安全基线，所有后续设计都必须符合这些原则。这些原则是不可妥协的设计红线。

### 2.1 安全原则清单

| 编号 | 原则 | 说明 | 对标 Nezha 漏洞 |
|------|------|------|----------------|
| S1 | **纯只读架构** | Server 到 Agent 方向不存在任何控制通道。Agent 只采集和上报，Server 不下发任何指令。 | 根除 CVE-2026-46716（Cron RCE）、GHSA-q6xx-5vr8-p898（终端劫持）的攻击面 |
| S2 | **公网强制 TLS** | 所有公网通信（Agent↔Server、浏览器↔Server）强制加密，不允许关闭。Agent 始终连接 `wss://` 公网域名并拒绝公网明文 Server。唯一例外：Server 绑定回环地址、由受信隧道（Cloudflare Tunnel）终结公网 TLS 时，回环段允许明文；该模式不开放公网明文端口。 | 根除 NEZHA-AGENT-001（明文 gRPC 凭证泄露），同时兼容 Cloudflare 入口 |
| S3 | **非 root 运行** | Agent 始终以非特权用户运行，通过 setcap 获取最小能力（仅 ICMP），不支持则降级 TCP Ping。 | 降低 RCE 发生时的危害 |
| S4 | **无远程执行** | 代码中不存在任何"执行命令""建立终端会话""操作文件"的功能。这是 S1 的代码级保障。 | 根除所有 RCE 类漏洞的根源 |
| S5 | **单管理员 + 强认证** | 第一版单管理员，强密码 + 登录限速 + 可选 TOTP。不引入多租户，避免 BOLA/IDOR 类漏洞。 | 根除 CVE-2026-46716 等 9 个漏洞的根因（多租户授权缺失） |
| S6 | **SSRF 防护** | 所有对外发起 HTTP 请求的功能（Webhook 通知、OAuth2 回调）做内网地址过滤、禁止重定向到内网、限制响应体大小。 | 针对 GHSA-w4g9-mxgg-j532（SSRF + 响应体反射） |
| S7 | **最小权限采集** | Agent 只采集不需要 root 的数据。需要 root 的数据（如 SMART、其他用户进程详情）不采集或标注"不可用"。 | 减少攻击面 |
| S8 | **配置文件权限控制** | Agent 配置文件（含 Token）权限 600，仅属主可读写。 | 防止本地凭证泄露 |
| S9 | **受限探测通道** | Agent 发起的 HTTP Ping 仅允许固定形态：GET/HEAD、固定 UA、禁止自定义 header/body、响应体不读取不存储、重定向 ≤2、单 Agent HTTP 目标数上限 32（全部目标 ≤64）、每目标串行。即使 Server 被攻破，探测配置也无法被用作控制通道。 | 堵住"配置污染驱动 Agent 行为"通道（Nezha CVE-2026-47120 变体，见 T13） |

### 2.2 数据流方向（关键安全特性）

```
Agent ────采集数据 + 探测结果──────────▶ Server  (主动推送, WS)
Agent ◀───探测目标配置(只读JSON)────────  Server  (Agent 主动拉取, HTTPS GET)
浏览器 ────查看请求─────────────────────▶ Server  (REST API)
浏览器 ◀───实时数据推送────────────────  Server  (WebSocket)
```

**关键点**：Server 永远不会主动向 Agent 发起连接。Agent 主动连接 Server 并维持 WebSocket，数据流是 Agent→Server 单向。配置同步也是 Agent 主动发起 HTTPS GET 拉取，不是 Server 推送。这意味着即使 Server 被攻破，攻击者也无法通过 Server 向 Agent 下发任何东西——因为 WebSocket 连接是 Agent 发起的，且协议中只定义了 Agent→Server 的数据帧和 Server→Agent 的配置响应帧，没有"命令"帧。

唯一能影响 Agent 行为的输入是探测目标配置（Agent 定时 REST 拉取的只读数据），它受 S9 形态约束：即使 Server 被攻破，攻击者也无法借助它把 Agent 变成任意 HTTP 请求的发送器（见 7.1 T13）。

---

## 3. 系统架构

### 3.1 系统全景

```
                    ┌─────────────────────────────────────────────┐
                    │              浏览器 (用户)                    │
                    │   React 前端 (内嵌于 Server 二进制)           │
                    └──────────────────┬──────────────────────────┘
                                       │ HTTPS (wss + REST API)
                                       ▼
┌──────────────────────────────────────────────────────────────────────┐
│                        Server (服务端, 单二进制)                       │
│  ┌─────────────┐  ┌──────────────┐  ┌──────────────┐  ┌──────────┐  │
│  │  API 层     │  │  业务层       │  │  数据层       │  │ 前端静态 │  │
│  │ (net/http)  │  │  (service/)  │  │  (repo/)     │  │ (embed)  │  │
│  │  - REST     │  │  - monitor   │  │  - SQLite    │  │          │  │
│  │  - WebSocket│  │  - alert     │  │  - RingBuffer│  │          │  │
│  │  - Auth     │  │  - notify    │  │              │  │          │  │
│  │             │  │  - config    │  │              │  │          │  │
│  └──────┬──────┘  └──────────────┘  └──────────────┘  └──────────┘  │
│         │                                                             │
└─────────┼─────────────────────────────────────────────────────────────┘
          │ WebSocket (wss, 强制TLS)
          │ Agent → Server 单向数据流
          ▲
          │
┌─────────┼─────────────────────────────────────────────────────────────┐
│         │              Agent (客户端, 单二进制, 非root)                  │
│  ┌──────┴──────┐  ┌──────────────┐  ┌──────────────┐                  │
│  │  上报器      │  │  采集器组     │  │  配置拉取器   │                  │
│  │  (WS客户端)  │  │  - cpu/mem   │  │  (定时同步    │                  │
│  │  - 心跳维持  │◀│  - disk/net  │  │   探测目标)   │                  │
│  │  - 数据上报  │  │  - ping      │  │              │                  │
│  └─────────────┘  └──────────────┘  └──────────────┘                  │
└──────────────────────────────────────────────────────────────────────┘
```

### 3.2 技术栈

| 层级 | 技术 | 理由 |
|------|------|------|
| 后端语言 | Go 1.26.9 | 单二进制、跨平台、并发强、Nezha/Komari 验证过；与 M1 代码一致 |
| HTTP/WS | 标准库 `net/http` + `github.com/coder/websocket` | Go 1.22+ 路由模式足够；少一层框架依赖，WS 库维护活跃；M2 已按此实现 |
| 通信协议 | WebSocket + JSON（强制 wss） | 强制 TLS、复用 HTTPS 证书、JSON 易调试 |
| 数据访问 | `database/sql` + `modernc.org/sqlite` | 纯 Go，免 cgo 交叉编译；内嵌顺序迁移；显式 SQL 便于审计，禁用 GORM AutoMigrate（与 M1 实现一致） |
| 数据库 | SQLite（内嵌） | 零配置、单文件、适合个人十几台规模 |
| 实时数据 | 内存环形缓冲 | 吸收高频写入，前端读内存快 |
| 前端框架 | React 19 + TypeScript | 生态成熟、组件化适合主题/布局/分享 |
| 前端构建 | Vite 8 | 快速构建，产物供 Go embed |
| UI/样式 | 自定义 CSS 设计系统 | 内置 7 套主题（3 套新版 + 4 套经典）；不引入 Tailwind/shadcn，减少依赖与供应链面 |
| 图表 | 自定义 SVG | CPU/内存折线、延迟格子；无第三方图表运行时 |
| 状态管理 | React hooks | 单页规模足够，避免额外状态库 |
| ICMP 库 | pro-bing（prometheus-community） | Nezha 同款、活跃维护、统计信息丰富 |

### 3.3 Server 目录结构

```
server/
├── cmd/
│   └── server/
│       └── main.go              # 入口, 初始化配置/数据库/路由, 启动服务
├── internal/
│   ├── api/                     # API层 (HTTP路由处理)
│   │   ├── router.go            # 路由注册
│   │   ├── middleware.go        # 中间件(认证/限速/请求日志)
│   │   ├── handler_auth.go      # 登录/登出/TOTP
│   │   ├── handler_server.go    # 服务器列表/详情
│   │   ├── handler_agent.go     # Agent WebSocket接入/注册/配置下发
│   │   ├── handler_alert.go     # 告警规则CRUD
│   │   ├── handler_notify.go    # 通知渠道CRUD
│   │   ├── handler_config.go    # 探测目标/系统设置
│   │   └── handler_public.go    # 公开分享页
│   ├── service/                 # 业务层
│   │   ├── monitor.go           # 实时数据管理(Agent连接池+环形缓冲)
│   │   ├── alert.go             # 告警引擎(阈值检测+状态机)
│   │   ├── notify.go            # 通知发送(SSRF防护+去重+静默期)
│   │   ├── agent_registry.go    # Agent注册/Token管理/注册码
│   │   └── config_sync.go       # 探测目标配置同步
│   ├── repository/              # 数据层
│   │   ├── sqlite.go            # SQLite连接管理
│   │   ├── repo_agent.go        # Agent元数据CRUD
│   │   ├── repo_alert.go        # 告警规则CRUD
│   │   ├── repo_notify.go       # 通知渠道CRUD
│   │   ├── repo_record.go       # 历史聚合数据CRUD
│   │   └── ringbuffer.go        # 内存环形缓冲(实时数据)
│   ├── model/                   # 数据模型(共享)
│   │   ├── agent.go             # Agent/ServerInfo
│   │   ├── metric.go            # 监控指标结构
│   │   ├── alert.go             # 告警规则模型
│   │   └── notify.go            # 通知渠道模型
│   └── pkg/                     # 工具包
│       ├── auth.go              # 管理会话/密码哈希/TOTP
│       ├── ssrf.go              # SSRF防护(内网过滤/重定向检测)
│       ├── tls.go               # 强制TLS/证书管理
│       └── embed.go             # 前端静态资源embed
├── frontend/                    # React前端(独立开发)
│   ├── package.json
│   └── src/
├── web/                         # 前端构建产物(唯一embed源, 被pkg/embed.go引用; cd frontend && npm run build 输出到此处)
│   └── (构建后生成)
├── go.mod
└── go.sum
```

### 3.4 Agent 目录结构

```
agent/
├── cmd/
│   └── agent/
│       └── main.go              # 入口, 加载配置, 启动各模块
├── internal/
│   ├── collector/               # 采集器组
│   │   ├── cpu.go               # CPU使用率/核心数/型号/负载
│   │   ├── memory.go            # 内存/Swap使用率
│   │   ├── disk.go              # 磁盘分区使用率/IO
│   │   ├── network.go           # 网卡流量/TCP UDP连接数
│   │   ├── system.go            # 运行时间/系统信息/Agent版本
│   │   ├── gpu.go               # GPU使用率(如可用, 非root)
│   │   └── ping.go              # 三网延迟/丢包(ICMP或TCP)
│   ├── reporter/                # 上报器
│   │   ├── ws.go                # WebSocket客户端, 维持长连接
│   │   ├── heartbeat.go         # 心跳维持(每30秒)
│   │   └── upload.go            # 数据打包上报(每3秒)
│   ├── config/                  # 配置拉取器
│   │   └── sync.go              # 定时HTTPS GET拉取探测目标配置
│   └── register/                # 注册器
│       └── register.go          # 首次启动用注册码换取持久Token
├── go.mod
└── go.sum
```

---

## 4. Agent 详细设计

### 4.1 采集项清单与权限要求

| 采集项 | 数据来源 | 是否需要root | 备注 |
|--------|---------|-------------|------|
| CPU使用率 | `/proc/stat` | 否 | 读取即可 |
| CPU型号/核心数 | `/proc/cpuinfo` | 否 | 读取即可 |
| 负载(1/5/15分) | `/proc/loadavg` | 否 | 读取即可 |
| 内存/Swap | `/proc/meminfo` | 否 | 读取即可 |
| 磁盘使用率 | 系统调用(statfs) | 否 | 读取即可 |
| 磁盘IO | `/proc/diskstats` | 否 | 读取即可 |
| 网卡流量速率 | `/proc/net/dev` | 否 | 两次采样差值计算速率 |
| **月度流量累计** | `/proc/net/dev` | 否 | Agent 维护累计字节数并持久化到 `state_file`（重启后从持久化值续算），Server 按自然月聚合，用于流量配额展示 |
| TCP/UDP连接数 | `/proc/net/tcp`, `/proc/net/udp` | 否 | 读取即可 |
| 进程数 | `/proc` 遍历 | 否 | 只统计数量,不读其他用户进程详情 |
| 运行时间 | `/proc/uptime` | 否 | 读取即可 |
| 系统信息 | `uname` 系统调用 | 否 | 读取即可 |
| **IPv4/IPv6 地址** | 网卡地址/出站连接探测 | 否 | 注册时上报公网出口 IPv4/IPv6（通过连接 Server 的本地 socket 获取，无需访问外部服务），运行时定期更新 |
| GPU使用率 | NVML 库绑定（go-nvml） | 否 | 仅 NVIDIA；cgo 直读，**禁止**调用 nvidia-smi 等外部命令（S4 要求 Agent 零 exec） |
| ICMP Ping | 原始套接字 | 需`CAP_NET_RAW` | setcap赋予,不支持则降级TCP Ping |
| TCP Ping | TCP连接 | 否 | 降级方案,测TCP握手延迟 |
| 温度 | `/sys/class/thermal` | 否 | 如可用,读取即可 |

**IPv6 说明**：公网 IP 获取方式为「Agent 与 Server 建立 WebSocket/TLS 连接时，读取本地 socket 的 RemoteAddr」——这是连接 Server 的真实出口 IP，无需调用任何外部 IP 查询服务，零额外网络请求，符合安全原则。IPv6 探测目标（如 `v6` 标记的目标）仅在 Agent 具备 IPv6 出口时执行，否则该目标显示"无 IPv6"。

**平台支持范围**：v1 仅支持 Linux——采集实现基于 `/proc` 与 Linux 系统调用，安装/降级依赖 systemd。Windows/macOS 支持列入 v2 计划（届时改用 gopsutil 跨平台采集，功能子集仅含基本信息 + TCP Ping），本版发布矩阵不包含它们。

### 4.2 一键安装上线流程

参考 Komari 的一键复制运行命令体验，增强注册码安全性。

```
管理员在面板上                    被控服务器上
┌──────────────┐                ┌──────────────┐
│ 1. 点击"添加  │                │              │
│    服务器"    │                │              │
│ 2. 生成一次性 │                │              │
│    注册码     │                │              │
│ 3. 显示一键   │ ──复制命令──▶  │ 4. 粘贴执行   │
│    安装命令   │                │ 5. 脚本检测架构│
│              │                │ 6. 下载Agent  │
│              │                │ 7. 创建probe用户│
│              │                │ 8. setcap     │
│              │                │ 9. 安装服务    │
│              │                │10. 启动Agent  │
│              │                │11. HTTPS REST │
│              │                │   注册换Token │
│ 12. 收到注册  │ ◀──WebSocket──│12. wss连接    │
│    服务器上线 │                │   持续上报    │
└──────────────┘                └──────────────┘
```

**一键命令示例**：

```bash
curl -fsSL https://your-server.com/install.sh | bash -s -- --server https://your-server.com --code K7F2-9QXM-4T8B-R6NC-2W5D-8HJA-3P
```

**注册码安全机制**（比 Komari 更安全）：

| 属性 | 设计 |
|------|------|
| 熵 | 128-bit 随机（Base32 分组展示），服务端只存 SHA-256 哈希 |
| 有效期 | 生成后 15 分钟内有效，超时自动失效 |
| 使用次数 | 一次性，注册成功后立即失效 |
| 服务端存储 | 只存 SHA-256 哈希；管理页只显示 id/状态/过期时间，不回显注册码 |
| 绑定信息 | 注册成功后绑定 Agent 的主机指纹（install_salt + CPU 型号 + 主网卡 MAC + GOOS 的 SHA-256），防止 Token 被盗用到其他机器 |
| 数量限制 | 每个管理员最多 5 个未使用的注册码同时存在 |
| 传输安全 | 注册码经 HTTPS REST 传输（POST /api/v1/agent/register），不裸露在明文 HTTP 中；该端点限速 5 次/分钟/IP，防 15 分钟窗口内爆破 |

**注册流程**：
1. Agent 首次启动，配置文件中只有 Server 地址和注册码，无 Token
2. Agent 通过 HTTPS REST 调用 POST /api/v1/agent/register，提交注册码 + 主机基本信息（含主机指纹）
3. Server 验证注册码有效性和有效期，生成持久 Token，返回给 Agent
4. Agent 将 Token 保存到配置文件（权限 600），注册码从配置中删除
5. 后续所有通信使用 Token 认证
6. Server 标记注册码已使用，记录 Agent 的主机指纹

### 4.3 Agent 配置文件

```yaml
# /etc/probe-agent/config.yml (权限: 600, 属主: probe)
server: "https://your-server.com"    # Server地址
token: "persistent-token-xxx"        # 注册后获得,首次安装时为空
# register_code: "ABC123XY"          # 首次安装时有,注册后删除
install_salt: "随机64位hex"           # 安装时生成,参与主机指纹计算(防跨机盗用)
tls_pin_mode: "none"                 # none|spki_sha256; Cloudflare/公网 CA 用 none, 直连自签才启用
server_cert_fingerprints: []         # 仅 tls_pin_mode=spki_sha256 时使用; 指纹必须由一键命令带外传入, 不得从同一 URL 现取
tls_insecure: false                  # 仅本地调试,生产禁用;开启时面板标注"证书未验证"
state_file: "/var/lib/probe-agent/state.json"  # 流量累计等状态持久化(属主probe,权限600)
report_interval: 3                   # 上报间隔(秒)
config_sync_interval: 3600           # 配置拉取间隔(秒)
ping_method: "auto"                  # auto/icmp/tcp, auto=优先ICMP降级TCP
```

### 4.4 Agent 上报数据格式（JSON over WebSocket）

```json
{
  "type": "report",
  "timestamp": 1718900000,
  "hostname": "web-server-01",
  "agent_version": "1.0.0",
  "data": {
    "cpu": {
      "usage": 45.2,
      "cores": 4,
      "model": "Intel Xeon E5-2680",
      "load_1": 0.52,
      "load_5": 0.48,
      "load_15": 0.50
    },
    "memory": {
      "total": 8589934592,
      "used": 4294967296,
      "swap_total": 4294967296,
      "swap_used": 0
    },
    "disk": [
      {"device": "/", "total": 53687091200, "used": 26843545600},
      {"device": "/data", "total": 107374182400, "used": 53687091200}
    ],
    "network": {
      "rx_speed": 1048576,
      "tx_speed": 524288,
      "tcp_connections": 128,
      "udp_connections": 16
    },
    "traffic_monthly": {
      "month": "2026-08",
      "rx_bytes": 107374182400,
      "tx_bytes": 53687091200
    },
    "ip_info": {
      "ipv4": "1.2.3.4",
      "ipv6": "2408:8207::1"
    },
    "uptime": 86400,
    "process_count": 156
  }
}
```

**`traffic_monthly` 说明**：Agent 本地维护「当月累计收发字节数」。计数器规则：
- Agent 重启后从配置文件/本地状态文件中恢复上次累计值，续算而非清零（`/proc/net/dev` 计数器重启归零，需 Agent 自行持久化）
- 跨自然月时归零重新累计
- Server 端以「当月内收到的最大累计值」为准（避免乱序），按月落盘归档
- 该数据用于面板的月流量进度条和超量告警
- 时区规则：跨月归零与 `month` 字段均按 **Agent 本地时区**计算，Server 按上报的 month 字符串归档、不做事后换算；与 VPS 商家账单（常按其所在时区）可能存在 ±1 天误差，属预期行为
- 状态持久化（state.json）写盘**节流至 60 秒**一次，避免 3 秒级高频写盘磨损 SSD；崩溃时丢失窗口内的流量增量不计（可接受，跨月边界自动修正）

### 4.5 Ping 探测数据格式

```json
{
  "type": "ping_result",
  "data": [
    {
      "target": "114.114.114.114",
      "name": "电信",
      "method": "icmp",
      "ip_version": 4,
      "avg_latency": 12.5,
      "min_latency": 10.2,
      "max_latency": 15.8,
      "jitter": 1.8,
      "loss": 0.0,
      "packets_sent": 10,
      "packets_recv": 10
    },
    {
      "target": "2400:3200::1",
      "name": "移动v6",
      "method": "icmp",
      "ip_version": 6,
      "avg_latency": 8.3,
      "min_latency": 7.1,
      "max_latency": 10.5,
      "jitter": 0.9,
      "loss": 10.0,
      "packets_sent": 10,
      "packets_recv": 9
    }
  ]
}
```

### 4.5.1 探测目标命名规则（学 NodeGet，支持线路自动识别）

为了让前端能自动识别线路/协议/IP 类型并分组展示，探测目标支持结构化命名约定（也可在面板上直接为每个目标配置 `region`/`isp`/`ip_version`/`protocol` 元数据，面板配置优先于命名解析）：

```
格式: 城市-运营商-v4/v6.域名[:端口]
示例: sh-cu-v4.ip.example.com:80

识别规则:
| 字段    | 含义                |
|---------|---------------------|
| v4      | IPv4                |
| v6      | IPv6                |
| 带端口  | TCP Ping            |
| 不带端口 | ICMP Ping          |
| ct      | 电信                |
| cu      | 联通                |
| cm      | 移动                |
| 城市缩写 | sh上海/bj北京/gz广州 等 |
```

前端按「运营商 → 城市」自动分组延迟数据，展示为 `上海电信`、`福建移动`、`上海联通` 等具体名称（NodeGet 社区主题的成熟做法）。

### 4.6 Ping 探测方案（超越 Nezha）

#### Nezha 更准确的 5 个原因

| 原因 | Nezha | Komari | 影响 |
|------|-------|--------|------|
| ICMP 采样数 | 5 个包取平均 | 1 个包 | 最关键，5 包平均能平滑抖动、容忍部分丢包 |
| 超时时间 | 20 秒 | 3 秒 | 高延迟链路上 Komari 易误判失败 |
| pro-bing 版本 | v0.9.0 | v0.7.0 | 新版有 bug 修复和精度改进 |
| 重试策略 | 无（直接报告） | 高延迟重试 | Komari 的重试系统性丢弃真实高延迟数据，引入偏差 |
| 丢包率统计 | 5 包中部分丢包仍成功 | 单包丢失即失败 | Komari 丢包率被放大 |

#### 我们的 Ping 方案设计

**ICMP Ping 参数**：

| 参数 | 我们的设计 | Nezha | Komari | 设计理由 |
|------|-----------|-------|--------|---------|
| 采样数 | **10 个包** | 5 | 1 | 10 包的标准误是 5 包的 1/√2 ≈ 71%，统计更稳定 |
| 发包间隔 | **0.5 秒** | 1 秒(默认) | - | 0.5 秒能在 5 秒内完成 10 包，比 Nezha 的 5 秒更快出结果 |
| 总超时 | **15 秒** | 20 秒 | 3 秒 | 10 包×0.5 秒=5 秒，15 秒超时留足余量 |
| DNS 解析 | **预解析排除** | 预解析 | 预解析 | 排除 DNS 时间，只测网络延迟 |
| 特权模式 | **优先 privileged**，降级 unprivileged | privileged | privileged | Linux 3.0+ 支持 unprivileged ICMP |

**延迟和丢包率计算**：

| 指标 | 计算方式 | 对比 Nezha |
|------|---------|-----------|
| **平均延迟** | 10 个包的 AvgRtt | Nezha 只有 5 包平均，我们更稳定 |
| **最小延迟** | MinRtt | Nezha 不报告，我们额外提供 |
| **最大延迟** | MaxRtt | Nezha 不报告，我们额外提供 |
| **抖动(Jitter)** | StdDevRtt | Nezha 不报告，反映网络稳定性 |
| **丢包率** | `(PacketsSent - PacketsRecv) / PacketsSent × 100%` | 每次探测直接报告精确丢包率 |

**TCP Ping 参数**（ICMP 不可用时的降级方案）：

| 参数 | 我们的设计 | Nezha | Komari |
|------|-----------|-------|--------|
| 采样数 | **5 次**取平均 | 1 次 | 1 次(有重试) |
| 超时 | **5 秒**/次 | 10 秒 | 3 秒 |
| 间隔 | **0.5 秒** | - | - |
| 延迟计算 | 5 次平均 | 单次 | 单次 |
| 丢包率 | 5 次中失败比例 | 成功/失败 | -1 比例 |

**HTTP Ping 参数**：

| 参数 | 我们的设计 | Nezha | Komari |
|------|-----------|-------|--------|
| 采样数 | **3 次**取平均 | 1 次 | 1 次 |
| 超时 | **10 秒**/次 | 30 秒 | 3 秒 |
| DNS 解析 | **排除**（自定义 DialContext） | 包含 | 排除 |
| 延迟计算 | 3 次平均 | 单次 | 单次 |
| 状态码判定 | 2xx-3xx 成功 | 2xx-399 | 2xx-3xx |

**HTTP Ping 安全收紧（S9 受限探测通道，T13 防御）**——这些约束是硬编码的，不可通过配置改变：

| 约束 | 规定 |
|------|------|
| 请求方法 | 仅 GET/HEAD，硬编码，不可配置 |
| 请求头 | 固定 UA（`probe-agent/<version>`），禁止自定义任何 header |
| 请求体 | 禁止 |
| 响应体 | 不读取、不存储、不上报（仅依据状态码判定成功/失败） |
| 重定向 | 最多跟随 2 跳，每跳重新校验 http/https |
| 目标数上限 | 单 Agent HTTP 目标 ≤ 32，全部探测目标合计 ≤ 64（Server 下发前校验，Agent 端兜底截断） |
| 并发 | 每目标 3 次采样串行进行 |

**关键改进点**（相比 Nezha 的优势）：

1. **每次探测直接报告精确丢包率**——发 10 个包，直接报告"10 包中收到 8 包，丢包率 20%"
2. **报告抖动(Jitter)**——StdDevRtt 反映延迟波动程度
3. **无偏差处理**——不采用 Komari 的高延迟重试策略，原样报告所有数据
4. **unprivileged ICMP 降级**——privileged（setcap）→ unprivileged（内核支持）→ TCP Ping

**探测调度策略**：
- Ping 探测独立于常规监控数据上报，默认每 **60 秒**执行一轮完整探测
- 探测结果随下一次常规数据上报一起发送，或探测完成后立即单独上报
- 探测间隔可在服务端配置（管理员可调整为 30 秒或 120 秒）
- **目标间并发执行**：全局 semaphore 默认 8（Server 配置可调）。理由：HTTP Ping 单目标最坏 3×10s=30s、ICMP 总超时 15 秒，目标串行时 20 个目标就会挤爆 60 秒周期，导致延迟格子出现空洞
- 目标数上限：单 Agent 全部探测目标 ≤ 64（HTTP 目标 ≤ 32），Server 下发前校验，Agent 端兜底截断

### 4.7 配置拉取（只读数据同步）

Agent 每小时通过 HTTPS GET 拉取探测目标配置，这是纯只读的数据同步，不是控制指令：

```
Agent ────GET /api/v1/agent/config
          Header: Authorization: Bearer <token>────────▶ Server
Agent ◀───{"ping_targets": [{"target":"114.114.114.114","name":"电信"}]}─── Server
```

- Token 通过 `Authorization` 请求头传输，**禁止放入 URL query**——避免 Token 落入反向代理访问日志（呼应 S8）
- Server 不主动推送配置，WebSocket 上不存在任何 Server→Agent 的配置下发帧（见 5.2）
- Agent 本地缓存这份配置，按配置执行探测。如果拉取失败，使用上次的缓存配置

### 4.8 Agent 权限降级流程

```
安装脚本
    │
    ▼
创建probe用户 (useradd -r -s /usr/sbin/nologin probe)
    │
    ▼
下载Agent二进制
    │
    ▼
尝试 setcap cap_net_raw+ep ./agent
    │
    ├─ 成功 ──▶ ICMP Ping (privileged模式), 标记 ping_method=icmp
    │
    └─ 失败 ──▶ 尝试 unprivileged ICMP (Linux 3.0+)
                    │
                    ├─ 成功 ──▶ ICMP Ping (unprivileged模式), 标记 ping_method=icmp_unprivileged
                    │
                    └─ 失败 ──▶ TCP Ping, 标记 ping_method=tcp
    │
    ▼
安装systemd service (User=probe, 无root)
    │
    ▼
启动Agent
```

> **unprivileged ICMP 前置条件**：SOCK_DGRAM ICMP 要求内核参数 `net.ipv4.ping_group_range` 覆盖 probe 用户的 GID（多数发行版默认 `1 0` 即禁用）。安装脚本尝试 `sysctl -w net.ipv4.ping_group_range="0 2147483647"`，写入失败（容器/无权限）则如实标注降级结果，不做静默重试。
>
> **ICMP 能力授予方式**：systemd unit 通过 `AmbientCapabilities=CAP_NET_RAW` 授予——`NoNewPrivileges=true` 会阻止 execve 时获得 file capabilities，因此 **setcap 与 NoNewPrivileges 互斥**（setcap 后的二进制在该 unit 下会静默失去 CAP_NET_RAW 并降级 TCP Ping）。setcap 仅作为非 systemd 场景的备用方案保留。

**systemd service 文件**：

```ini
[Unit]
Description=Server Probe Agent
After=network.target

[Service]
Type=simple
User=probe
Group=probe
ExecStart=/usr/local/bin/probe-agent --config /etc/probe-agent/config.yml
Restart=always
RestartSec=5
# ICMP 能力由 systemd 授予: NoNewPrivileges 会阻止 file capabilities 生效,
# setcap 与 NoNewPrivileges 互斥(会静默失去 CAP_NET_RAW), 故用 AmbientCapabilities
AmbientCapabilities=CAP_NET_RAW
CapabilityBoundingSet=CAP_NET_RAW
# 安全加固
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
PrivateDevices=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictAddressFamilies=AF_INET AF_INET6 AF_NETLINK AF_UNIX
RestrictSUIDSGID=true
MemoryMax=128M
ReadWritePaths=/etc/probe-agent /var/lib/probe-agent

[Install]
WantedBy=multi-user.target
```

---

## 5. Server 详细设计

### 5.1 REST API 端点

| 方法 | 路径 | 认证 | 功能 | 安全要点 |
|------|------|------|------|---------|
| GET | `/healthz` | 无 | 健康检查 | 只返回 ok/version |
| POST | `/api/v1/setup` | 无 | 首次初始化管理员 | setup token 30 分钟一次性；admin 非空后返回 404 |
| POST | `/api/v1/auth/login` | 无 | 登录 | 限速5次/分钟/IP, 连续10次失败锁定15分钟, Argon2id 验证 |
| POST | `/api/v1/auth/logout` | 管理会话 | 登出 | 清除Cookie, 并吊销服务端会话记录（见 7.3） |
| GET | `/api/v1/auth/me` | 管理会话 | 当前管理员 | 只读；不返回密码/TOTP 密钥 |
| POST | `/api/v1/auth/totp/verify` | 管理会话 | TOTP验证 | 登录第二步 |
| GET | `/api/v1/servers` | 管理会话 | 服务器列表 | 支持标签/地区/关键词筛选参数 |
| GET | `/api/v1/servers/:id` | 管理会话 | 单台详情(含历史) | - |
| GET | `/api/v1/servers/:id/history` | 管理会话 | 历史趋势数据 | 按时间范围查询 |
| PUT | `/api/v1/servers/:id/meta` | 管理会话 | 更新服务器元数据 | 仅管理员; 名称/标签/位置/到期/成本等 |
| GET | `/api/v1/servers/:id/traffic` | 管理会话 | 月度流量统计 | 按月查询收发累计 |
| GET | `/api/v1/tags` | 管理会话 | 标签列表 | - |
| POST | `/api/v1/tags` | 管理会话 | 创建标签 | 仅管理员 |
| PUT | `/api/v1/tags/:id` | 管理会话 | 修改标签 | 仅管理员 |
| DELETE | `/api/v1/tags/:id` | 管理会话 | 删除标签 | 仅管理员 |
| POST | `/api/v1/agent/register` | 注册码 | Agent注册 | 注册码只存哈希、一次性、15分钟有效；Token 只在响应中返回一次；主机指纹冲突返回409；限速5次/分钟/IP |
| GET | `/api/v1/agent/config` | Agent Token (Authorization头) | 拉取探测目标配置 | 只读JSON, 无控制指令; Token禁止入URL |
| GET | `/api/v1/server-cert` | 无 | 获取Server证书SHA256指纹 | 可选，仅直连自签场景；不得作为 Pinning 的唯一信任来源；Cloudflare 模式不使用 |
| WS | `/api/v1/agent/report` | Agent Token | Agent WebSocket接入 | 仅接受Agent→Server数据帧 |
| GET | `/api/v1/alerts` | 管理会话 | 告警规则列表 | - |
| POST | `/api/v1/alerts` | 管理会话 | 创建告警规则 | 仅管理员 |
| PUT | `/api/v1/alerts/:id` | 管理会话 | 修改告警规则 | 仅管理员 |
| DELETE | `/api/v1/alerts/:id` | 管理会话 | 删除告警规则 | 仅管理员 |
| GET | `/api/v1/alerts/history` | 管理会话 | 告警历史时间线 | FIRING/RESOLVED 记录, 供告警页展示 |
| GET | `/api/v1/notify/channels` | 管理会话 | 通知渠道列表 | - |
| POST | `/api/v1/notify/channels` | 管理会话 | 添加通知渠道 | SSRF校验URL |
| PUT | `/api/v1/notify/channels/:id` | 管理会话 | 修改通知渠道 | SSRF校验URL |
| DELETE | `/api/v1/notify/channels/:id` | 管理会话 | 删除通知渠道 | - |
| GET | `/api/v1/config/ping-targets` | 管理会话 | 探测目标列表 | - |
| POST | `/api/v1/config/ping-targets` | 管理会话 | 添加探测目标 | - |
| PUT | `/api/v1/config/ping-targets/:id` | 管理会话 | 修改探测目标 | - |
| DELETE | `/api/v1/config/ping-targets/:id` | 管理会话 | 删除探测目标 | - |
| GET | `/api/v1/config/settings` | 管理会话 | 系统设置 | - |
| PUT | `/api/v1/config/settings` | 管理会话 | 修改系统设置 | 仅管理员 |
| GET | `/api/v1/agents/tokens` | 管理会话 | Agent Token 状态 | 只显示是否已配置，不回显 Token；重置时一次性显示新 Token |
| POST | `/api/v1/agents/register-codes` | 管理会话 | 生成注册码 | 最多5个未使用；明文只返回一次；服务端只存哈希 |
| GET | `/api/v1/agents/register-codes` | 管理会话 | 注册码列表 | 只返回 id/状态/过期时间，不返回明文/哈希 |
| DELETE | `/api/v1/agents/register-codes/:id` | 管理会话 | 删除未使用注册码 | - |
| GET | `/api/v1/system/status` | 管理会话 | 系统自身健康状态 | 供 8.8 自监控页使用 |
| GET | `/api/v1/public/:share_id` | 无 | 公开分享页 | share_id 为 UUIDv4 不可枚举; 只读, 无敏感信息 |
| WS | `/ws/public/:share_id` | share_id | 公开页实时数据推送 | 白名单字段; 同一 share_id 最多 5 个连接; 无登录入口 |
| GET | `/ws/dashboard` | 管理会话 | 面板实时数据推送 | WebSocket |

**M3–M6 实现补充端点**：`GET /api/v1/servers/{id}`、`GET /api/v1/servers/{id}/history?range=1h|6h|12h|1d|2d`、`GET /ws/dashboard`（管理会话）、`/api/v1/alerts`、`/api/v1/notify/channels`、`/api/v1/config/settings`、`/api/v1/sessions`、`/api/v1/audit`、`/api/v1/auth/totp/*`、`GET /api/v1/public/{share_id}`、`GET /install-agent.sh`、`GET /download/agent/{arch}/{file}`。均遵循同一套 Origin/会话/限速与安全头规则。

### 5.2 WebSocket 消息协议

**Agent → Server 帧**（仅这些类型，无其他；Token 在 WS 握手时经 Authorization 头校验，帧内不携带）：

```json
{"type": "report", "timestamp": 1718900000, "data": {...}}
{"type": "ping_result", "data": [...]}
{"type": "heartbeat", "timestamp": 1718900030}
```

**Server → Agent 帧**（仅确认帧，无任何配置下发）：

```json
{"type": "heartbeat_ack"}
```

**REST 辅助通道**（非 WebSocket，HTTPS，Token 走 Authorization 头）：
- POST `/api/v1/agent/register` —— 注册码换取 Token（替代旧 register 帧）
- GET `/api/v1/agent/config` —— Agent 定时拉取探测目标配置（替代旧 config_update 帧）
- GET `/api/v1/server-cert` —— 可选，仅直连自签模式；Pinning 指纹必须由管理员复制的一键命令带外传入，不得在同一条未认证连接里同时取证书和指纹；Cloudflare 模式不启用叶子证书 Pinning

**关键安全约束**：Server → Agent 的 WebSocket 方向**只有 1 种帧**（heartbeat_ack），不存在任何"下发配置""执行命令""建立会话""操作文件"的帧类型；配置变更一律由 Agent 定时 REST 拉取获得。这是 S1（纯只读架构）和 S4（无远程执行）的协议级保障。

### 5.3 实时数据管理

```
Agent WebSocket连接
        │
        ▼
┌───────────────────┐     ┌──────────────────────┐
│  Agent连接池        │     │  环形缓冲(每Agent一个) │
│  map[agentID]*Conn │     │  ├ CPU 最近3600点     │
│                   │────▶│  ├ 内存 最近3600点    │
│  维护在线状态       │     │  ├ 磁盘 最近3600点    │
│  心跳超时检测       │     │  ├ 网络 最近3600点    │
│                   │     │  └ Ping 最近60点     │
└───────────────────┘     └──────────────────────┘
        │                          │
        │                    每5分钟聚合
        │                          │
        │                          ▼
        │                  ┌──────────────┐
        │                  │ SQLite 落盘   │
        │                  │ (聚合历史数据) │
        └─────────────────▶└──────────────┘
        │
        ▼
┌───────────────────┐
│ 面板WebSocket推送   │
│ 浏览器实时数据      │
└───────────────────┘
```

- 环形缓冲大小：实时数据保留最近 3 小时（3 秒/点 × 3 小时 = **3600 点**，预分配 3600 点）——6H 视图由「3 小时实时缓冲 + 3 小时聚合数据」拼接而来（见 6.5）
- 聚合策略：每 5 分钟将当前实时数据聚合为一个点写入 SQLite，各字段规则：
  - CPU/内存/Swap/负载/连接数/进程数：取窗口内**平均值**
  - 磁盘使用率（JSON 按挂载点）：各挂载点分别取平均值，结构不变
  - 网络速率：取窗口内**平均速率**（缓冲中存的是速率而非累计字节，"网络取平均"）
  - Ping（JSON 按目标）：avg_latency 取均值，min/max 取极值，loss 与 jitter 取均值
- 历史数据保留：SQLite 中的聚合数据保留 90 天，超期自动清理
- 离线检测：Agent 心跳超时（默认 90 秒无心跳）标记为离线，触发离线告警

### 5.4 告警引擎

```
实时数据写入环形缓冲
        │
        ▼
┌───────────────────┐
│  告警状态机         │
│  每台Agent独立状态  │
│                   │
│  状态: OK / PENDING / FIRING / RESOLVED
│                   │
│  ┌─────────────┐  │
│  │ 检查阈值     │  │
│  │ CPU>80%?    │  │
│  │ 磁盘>90%?   │  │
│  │ 离线?       │  │
│  └──────┬──────┘  │
│         │         │
│  ┌──────▼──────┐  │
│  │ 持续时间检查 │  │     持续N秒触发
│  │ (防抖动)     │  │─────────────▶ FIRING
│  └──────┬──────┘  │
│         │         │
│  ┌──────▼──────┐  │
│  │ 通知去重     │  │     静默期内不重复通知
│  │ 静默期检查   │  │
│  └──────┬──────┘  │
│         │         │
│         ▼         │
│  调用 notify.go   │
└───────────────────┘
```

告警规则示例：

```json
{
  "id": 1,
  "name": "CPU高负载",
  "metric": "cpu_usage",
  "operator": ">",
  "threshold": 80,
  "duration": 300,
  "enabled": true,
  "notify_channel_id": 1
}
```

- `duration`：持续 N 秒超过阈值才触发，防抖动（默认 300 秒 = 5 分钟）
- 状态机：`OK → PENDING(超阈值但未达duration) → FIRING(达到duration) → RESOLVED(恢复正常)`
- 通知时机：进入 FIRING 时发送告警通知，进入 RESOLVED 时发送恢复通知
- 静默期：同一告警 FIRING 状态下，不重复发送通知，默认静默 60 分钟

### 5.5 通知发送（SSRF 防护重点）

```
告警触发
    │
    ▼
┌────────────────────────────────┐
│  SSRF 防护层                     │
│  ├ 解析URL的IP                  │
│  ├ 检查是否内网地址              │
│  │  ├ 10.0.0.0/8               │
│  │  ├ 172.16.0.0/12            │
│  │  ├ 192.168.0.0/16           │
│  │  ├ 127.0.0.0/8              │
│  │  ├ 169.254.0.0/16           │
│  │  ├ ::1/128                  │
│  │  └ fc00::/7                 │
│  ├ 禁止重定向到内网              │
│  └ 限制响应体大小(最多1KB, 超出截断)│
└────────────────┬───────────────┘
                 │ 通过
                 ▼
┌────────────────────────────────┐
│  通知发送                        │
│  ├ Webhook: POST JSON           │
│  ├ Telegram: Bot API            │
│  └ 邮件: SMTP                    │
└────────────────────────────────┘
```

**SSRF 防护要点**（针对 GHSA-w4g9-mxgg-j532）：

| 防护点 | 措施 | Nezha 的缺陷 |
|--------|------|-------------|
| 内网地址过滤 | 检查 10/8、172.16/12、192.168/16、127/8、169.254/16、::1、fc00::/7 | 无过滤 |
| DNS 重绑定 | 自定义 Dialer 强制使用预解析 IP | 无防护 |
| 重定向攻击 | CheckRedirect 中再次 SSRF 检查 | 无防护 |
| 响应体反射 | 最多读 1KB，不反射给用户 | 无限制反射完整响应体 |
| 超时限制 | 10 秒 | 无限制 |
| TLS 验证 | 强制验证 | 可关闭（VerifyTLS=false） |
| SMTP 内网过滤 | SMTP host 保存与发送前解析并执行同一套内网地址检查（SMTP 无 CheckRedirect，需独立 Dialer 校验） | 无过滤 |

- Telegram 固定调用 `api.telegram.org`，不接受自定义域名，风险最低
- Webhook/SMTP 的内网过滤、DNS 预解析、超时逻辑共用同一实现，避免两套代码漂移

**通知模板（v1 固定模板 + 变量替换）**：告警与恢复各一套中/英文默认文案，支持变量 `{hostname}`、`{rule_name}`、`{metric}`、`{value}`、`{time}`。自定义模板列入后续规划（见 9.5）。

**通知渠道 config JSON 示例**（`password`/`bot_token` 等敏感字段存储于 SQLite，API 回显时一律脱敏为 `***`）：

```json
{"type": "webhook", "url": "https://hooks.example.com/xxx"}
{"type": "telegram", "bot_token": "123456:ABC", "chat_id": "-100123456"}
{"type": "smtp", "host": "smtp.example.com", "port": 465, "username": "ops@b.c", "password": "***", "from": "ops@b.c", "to": ["oncall@b.c"]}
```

### 5.6 SQLite 表结构

```sql
-- Agent元数据
CREATE TABLE agents (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    token_hash TEXT UNIQUE NOT NULL,      -- SHA-256(Agent Token); 明文 Token 只在创建/重置时返回一次
    hostname TEXT NOT NULL,
    display_name TEXT,                    -- 自定义显示名称(NodeGet风格, 管理员设置)
    os TEXT,
    arch TEXT,
    agent_version TEXT,
    host_fingerprint TEXT,                -- SHA256(安装盐+CPU型号+主网卡MAC+系统类型), 见7.5
    ipv4 TEXT,                            -- 出口IPv4(Agent上报)
    ipv6 TEXT,                            -- 出口IPv6(Agent上报)
    geo_lat REAL,                         -- GeoIP 解析纬度缓存(见5.7, 手动设置优先)
    geo_lon REAL,                         -- GeoIP 解析经度缓存(见5.7)
    region TEXT,                          -- 位置: "上海"/"Tokyo"/"US-LAX" (管理员设置)
    country_code TEXT,                    -- 国家代码: "CN"/"JP"/"US" (管理员设置, 用于旗帜+地图)
    isp TEXT,                             -- 供应商备注: "Bandwagon"/"Oracle" (管理员设置)
    tag_ids TEXT,                         -- JSON数组: 标签ID列表
    expires_at INTEGER,                   -- 到期时间戳(管理员设置, 用于到期展示与提醒)
    price_amount REAL,                    -- 周期费用数值(管理员设置)
    price_currency TEXT,                  -- 币种: CNY/USD/EUR/JPY
    price_cycle TEXT,                     -- 周期: monthly/yearly
    traffic_quota_bytes INTEGER,          -- 月流量配额字节数(0=不限)
    last_seen INTEGER,
    online INTEGER DEFAULT 0,
    created_at INTEGER NOT NULL,
    UNIQUE(host_fingerprint)
);

-- 标签
CREATE TABLE tags (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT UNIQUE NOT NULL,
    color TEXT,                           -- 标签颜色(前端展示)
    created_at INTEGER NOT NULL
);

-- 月度流量归档(每Agent每月一行, 由Agent上报的累计值归档)
CREATE TABLE traffic_monthly (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    agent_id INTEGER NOT NULL,
    month TEXT NOT NULL,                  -- "2026-08"
    rx_bytes INTEGER NOT NULL,            -- 当月最大累计接收字节数
    tx_bytes INTEGER NOT NULL,            -- 当月最大累计发送字节数
    UNIQUE(agent_id, month),
    FOREIGN KEY (agent_id) REFERENCES agents(id)
);

-- 注册码(只存哈希; UI 只显示 id/状态/过期时间)
CREATE TABLE register_codes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    code_hash TEXT UNIQUE NOT NULL,       -- SHA-256(注册码)
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    used INTEGER DEFAULT 0,
    used_by_agent_id INTEGER
);
CREATE INDEX idx_register_codes_expires ON register_codes(expires_at);

-- 告警规则
CREATE TABLE alert_rules (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    metric TEXT NOT NULL,
    operator TEXT NOT NULL,
    threshold REAL NOT NULL,
    duration INTEGER NOT NULL,
    enabled INTEGER DEFAULT 1,
    scope TEXT NOT NULL DEFAULT '{"type":"all"}',   -- 作用域: {"type":"all"} / {"type":"agent_ids","ids":[..]} / {"type":"tag_ids","ids":[..]}, 见5.4
    notify_channel_ids TEXT NOT NULL DEFAULT '[]',  -- 通知渠道ID JSON数组(一条规则可通知多渠道)
    created_at INTEGER NOT NULL
);

-- 告警历史(状态机持久化: Server重启后加载未RESOLVED记录恢复状态机, 避免重复通知, 见5.4)
CREATE TABLE alert_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    rule_id INTEGER NOT NULL,
    agent_id INTEGER NOT NULL,
    status TEXT NOT NULL,                 -- PENDING/FIRING/RESOLVED
    value REAL,                           -- 触发时指标值
    started_at INTEGER NOT NULL,          -- 首次进入PENDING时间
    updated_at INTEGER NOT NULL,
    notified INTEGER DEFAULT 0,           -- FIRING通知是否已发送
    FOREIGN KEY (rule_id) REFERENCES alert_rules(id)
);
CREATE INDEX idx_alert_history_agent ON alert_history(agent_id, updated_at);

-- 通知渠道
CREATE TABLE notify_channels (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    type TEXT NOT NULL,
    config TEXT NOT NULL,
    created_at INTEGER NOT NULL
);

-- 探测目标
CREATE TABLE ping_targets (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    target TEXT NOT NULL,                 -- IP或域名(可带端口表示TCP Ping)
    name TEXT NOT NULL,
    region TEXT,                          -- 地区标签: "上海"(用于前端分组展示)
    isp TEXT,                             -- 运营商: ct/cu/cm/other
    ip_version INTEGER DEFAULT 4,         -- 4/6
    protocol TEXT DEFAULT 'icmp',         -- icmp/tcp(带端口自动判定)
    is_default INTEGER DEFAULT 0,         -- 1=预置默认目标(种子数据, 可停用不可删除)
    enabled INTEGER DEFAULT 1,
    created_at INTEGER NOT NULL
);
```

**预置默认目标种子数据**（`is_default=1`，Server 首次启动写入；管理员可停用、不可删除，保证开箱即有探测数据，国际部署可自行替换）：

| target | name | ip_version | protocol |
|--------|------|-----------|----------|
| 114.114.114.114 | 电信 | 4 | icmp |
| 223.5.5.5 | 阿里 | 4 | icmp |
| 119.29.29.29 | 腾讯 | 4 | icmp |
| 2400:3200::1 | 阿里v6 | 6 | icmp |
| 2001:4860:4860::8888 | Google v6 | 6 | icmp |

```sql

-- 历史聚合数据(每5分钟一个点)
CREATE TABLE metric_records (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    agent_id INTEGER NOT NULL,
    timestamp INTEGER NOT NULL,
    cpu_usage REAL,
    mem_usage REAL,
    disk_usage TEXT,
    net_rx INTEGER,
    net_tx INTEGER,
    ping_data TEXT,
    FOREIGN KEY (agent_id) REFERENCES agents(id)
);
CREATE INDEX idx_metric_records_agent_time ON metric_records(agent_id, timestamp);

-- 管理会话(不透明令牌 + 服务端吊销: logout/改密后立即失效, 见7.3)
CREATE TABLE sessions (
    id TEXT PRIMARY KEY,                  -- 会话ID(随机 UUID, 仅用于管理页展示/吊销)
    token_hash TEXT UNIQUE NOT NULL,      -- SHA-256(Cookie 中的随机令牌)
    admin_id INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,          -- 绝对过期(默认 7d)
    idle_expires_at INTEGER NOT NULL,     -- 空闲过期(默认 2h)
    revoked INTEGER DEFAULT 0,
    created_at INTEGER NOT NULL,
    last_seen_at INTEGER,
    ip TEXT,
    user_agent TEXT,
    FOREIGN KEY (admin_id) REFERENCES admin(id)
);
CREATE INDEX idx_sessions_admin ON sessions(admin_id, expires_at);
CREATE INDEX idx_sessions_token_hash ON sessions(token_hash);

-- 管理员账户
CREATE TABLE admin (
    id INTEGER PRIMARY KEY,
    username TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    totp_secret TEXT,
    totp_enabled INTEGER DEFAULT 0,
    totp_recovery_codes TEXT,             -- 10个一次性恢复码(SHA256) JSON数组, 用后即删
    created_at INTEGER NOT NULL
);

-- 公开分享页配置
CREATE TABLE share_pages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    share_id TEXT UNIQUE NOT NULL,
    title TEXT,
    logo_url TEXT,                        -- 仅本地/内置资源路径, 不允许外链 URL
    footer_text TEXT,
    agent_ids TEXT,
    created_at INTEGER NOT NULL
);

-- 系统设置(主题选择、公开页开关、Cloudflare 相关展示配置等)
CREATE TABLE settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at INTEGER NOT NULL
);

-- M4/M5 追加（0002_history.sql）:
-- ping_records(agent_id, target, name, method, ip_version, timestamp,
--              avg_latency, min_latency, max_latency, jitter, loss,
--              packets_sent, packets_recv)
-- audit_log(timestamp, actor, action, target, detail)
-- metric_records 增加 (agent_id, timestamp) 唯一索引，用于 5 分钟桶 upsert。
```

### 5.7 地理位置与地图数据（GeoIP，离线方案）

地图视图（NodeGet 风格）需要服务器地理坐标。**采用完全离线方案**，Server 不对外发起任何 GeoIP 查询请求（符合安全原则）：

| 设计点 | 方案 |
|--------|------|
| IP → 国家/城市/坐标 | 内置 GeoLite2-City.mmdb 离线库（oschwald/geoip2-golang 读取），文件由管理员下载放入 data 目录，或首次安装脚本附带 |
| 数据更新 | 管理员手动替换 mmdb 文件（MaxMind 免费库每月更新），面板显示库版本与过期提示 |
| 优先级 | 管理员手动设置的 `region`/`country_code` > GeoIP 自动解析结果 |
| 坐标缓存 | 解析结果持久缓存到 agents 表的 `geo_lat`/`geo_lon` 字段（见 5.6），避免每次请求都读 mmdb |
| 前端地图 | ECharts + 世界 GeoJSON（前端静态资源内嵌，不走外部 CDN）；地图上以 `effectScatter` 光点展示节点位置，点击光点弹出该地区节点列表 |
| 无 mmdb 时 | 地图视图显示提示"未配置 GeoIP 数据库，请在设置页查看配置方法"，卡片/表格视图不受影响 |

**汇率折算**（到期成本多币种展示）：为避免 Server 对外请求汇率 API，采用管理员手动配置汇率（设置页可输入常用币种对 CNY 汇率），默认仅显示原始币种金额。可选增强：公开分享页由**浏览器端**直接请求免费汇率 API（不经过 Server，用户隐私自担），默认关闭。

### 5.8 服务器元数据管理

NodeGet 风格的元数据字段（全部由管理员在面板设置，Agent 无法上报覆盖）：

| 字段 | 用途 | 前端展示 |
|------|------|---------|
| display_name | 自定义名称 | 卡片标题（替代 hostname） |
| region / country_code | 位置 | 卡片国旗 emoji + 城市名；地图视图定位 |
| isp | 供应商 | 卡片副标题 |
| tag_ids | 标签 | 卡片彩色标签徽章；顶部筛选栏 |
| expires_at | 到期时间 | 卡片剩余天数（30天内黄色、7天内红色）；到期告警 |
| price_* | 费用 | 卡片小字展示；汇总页合计（按币种分组） |
| traffic_quota_bytes | 流量配额 | 月流量进度条；超量告警 |

**新增告警指标**：`traffic_quota`（月流量超过配额 80%/100% 触发）、`expire_days`（到期前 30/7 天提醒）——复用现有告警引擎状态机，无新增攻击面。

---

## 6. 前端面板设计（NodeGet 风格）

> **设计基调**：对标 NodeGet 及其社区主题（NIE-Theme 等）的视觉风格——玻璃拟态卡片、圆环指标、延迟小格子、柔和渐变配色、三视图切换。整体气质：清爽、现代、信息密度适中、深浅色双主题。

### 6.1 技术选型

| 维度 | 选择 | 理由 |
|------|------|------|
| 框架 | React 18 + TypeScript | 生态成熟，组件化模型适合主题/布局/分享功能 |
| 构建 | Vite | 快速构建，产物供 Go embed |
| UI 库 | shadcn/ui | 基于 Radix UI + Tailwind，可定制性强 |
| 图表 | ECharts | 性能好，支持实时数据流、世界地图、effectScatter |
| 状态管理 | Zustand | 轻量，适合中等复杂度应用 |
| 路由 | React Router | 标准选择 |
| 样式 | Tailwind CSS | 与 shadcn/ui 配套，主题切换方便 |
| 动效 | Tailwind CSS transitions + ECharts 动画 | 轻量动效，不引入重型动画库 |

### 6.2 设计语言（NodeGet 风格规范）

这是本次改版的核心，开发时必须严格遵循：

#### 视觉基调

| 设计要素 | 规范 |
|---------|------|
| **卡片** | 玻璃拟态：半透明背景 + `backdrop-blur` + 细边框 + 大圆角（`rounded-2xl`）+ 柔和阴影 |
| **圆环指标** | CPU/内存/磁盘用 SVG 小圆环（conic-gradient 或 SVG circle stroke-dashoffset），圆环中心显示百分比数字，圆环颜色随使用率渐变（绿→黄→红） |
| **延迟小格子** | VPS 圈经典组件：每行代表一个探测目标，行内 N 个小方格（最近 N 次探测），格子颜色按延迟分级渐变，**格子高度与延迟值成正比**（延迟越高格子越高），悬停显示具体数值与时间 |
| **配色** | 柔和渐变色调，延迟色阶：`#4ade80`(优) → `#a3e635`(良) → `#facc15`(中) → `#fb923c`(差) → `#f87171`(劣) → `#9ca3af`(超时/离线)。避免高饱和撞色，减少视觉割裂感 |
| **字体** | 系统字体栈 + tabular-nums（数字等宽，图表对齐），中英文混排优化 |
| **深浅色** | 双主题默认跟随系统，CSS 变量切换；深色为主推主题（VPS 圈审美主流） |
| **背景** | 可切换：纯色 / 内置柔和渐变（默认，如深蓝紫渐变）/ 本地上传背景图；背景之上叠加半透明卡片形成毛玻璃层次；公开页不接受外链背景 URL |
| **间距** | 卡片网格 gap-4，卡片内 padding-5，信息分区用细分隔线或留白，不堆砌边框 |

#### 交互规范

- 实时数据 3 秒推送，圆环和格子**平滑过渡动画**（CSS transition 300ms），数字变化不闪烁
- 卡片悬停轻微上浮 + 阴影加深（`hover:-translate-y-0.5`）
- 所有图表支持图例点击切换、悬停 tooltip 显示完整统计
- 移动端：卡片单列、顶部筛选收起为抽屉、表格视图横向滚动
- 页面不可见/窗口失焦/断网恢复时自动重新拉取数据（NodeGet 主题验证过的体验优化）

### 6.3 页面结构

```
┌─────────────────────────────────────────────────────────┐
│  顶栏: Logo | 站点标题 | 搜索 | 主题切换 | 用户菜单       │
├─────────────────────────────────────────────────────────┤
│  主内容区（顶部导航标签，非侧边栏——NodeGet 风格）        │
│  ├ 仪表盘（卡片/表格/地图三视图）                        │
│  ├ 告警                                                │
│  ├ 通知                                                │
│  ├ 设置                                                │
│  └ 公开页配置                                          │
└─────────────────────────────────────────────────────────┘
```

### 6.4 仪表盘（Dashboard）——三视图 + 筛选栏

#### 6.4.1 顶部筛选栏（所有视图共享）

```
┌──────────────────────────────────────────────────────────────┐
│ 概览条: 在线 8/10 · 离线 2 · 平均CPU 45% · 平均内存 60%       │
│        本月总流量 1.2TB · 月成本合计 $86.50 / ¥4200          │
├──────────────────────────────────────────────────────────────┤
│ [搜索🔍 服务器名/标签/位置] [标签▾] [地区▾] [IPv4/IPv6/全部▾]│
│ 排序[名称/CPU/内存/流量/延迟▾]        视图: [卡片] [表格] [地图]│
└──────────────────────────────────────────────────────────────┘
```

- 筛选条件组合生效，URL 参数持久化（刷新/分享保持筛选状态）
- 标签筛选显示彩色标签徽章；地区筛选按 country_code 分组（含国旗）
- 排序支持：名称 / CPU / 内存 / 磁盘 / 月流量 / 平均延迟 / 到期时间

#### 6.4.2 卡片视图（默认）

```
┌─────────────────────────────────────┐ ┌─────────────────────────────────────┐
│ 🇺🇸 US-LAX-01          [生产] [中转] │ │ 🇭🇰 HK-家宽-02        [家宽]         │
│ ● 在线 · 15天 · Bandwagon            │ │ ● 在线 · 3天 · Oracle              │
│                                     │ │                                     │
│   ◔ 45%      ◔ 60%      ◔ 70%      │ │   ◔ 12%      ◔ 30%      ◔ 50%      │
│   CPU        内存       磁盘        │ │   CPU        内存       磁盘        │
│                                     │ │                                     │
│ ↑ 1.2MB/s   ↓ 3.5MB/s   月流量▓▓▓░  │ │ ↑ 0.5MB/s   ↓ 1.0MB/s  月流量▓░░░  │
│ (圆环)      (圆环)      320G/1T     │ │ (圆环)      (圆环)     110G/500G   │
│─────────────────────────────────────│ │─────────────────────────────────────│
│ 延迟格子 (IPv4, 最近24次)            │ │ 延迟格子 (IPv4, 最近24次)           │
│ 上海电信 ▁▂▁▁▂▁▁▁▂▁▁▁▁▂▁▁▁▁▁▂▁▁  │ │ 上海电信 ▂▃▂▂▃▂▂▂▃▂▂▂▂▃▂▂▂▂▂▃▂▂  │
│ 上海联通 ▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁  │ │ 上海联通 ▁▁▁▁▂▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁  │
│ 上海移动 ▂▂▁▂▁▁▂▁▁▁▂▁▁▂▁▁▁▂▁▁▁  │ │ 上海移动 (无v6) ▁▁▂▁▁▁▂▁▁▁▂▁▁▁▁▂▁▁  │
│─────────────────────────────────────│ │─────────────────────────────────────│
│ 到期: 2027-01-15 (余178天)  $49.99/年│ │ 到期: 2026-09-01 (余6天⚠) ¥0 (白嫖)│
└─────────────────────────────────────┘ └─────────────────────────────────────┘
```

**卡片设计细则（NodeGet 风格核心）**：

| 区域 | 内容 | 组件 |
|------|------|------|
| 头部 | 国旗 emoji + display_name + 标签徽章 + 在线状态点 + 运行时间 + 供应商 | 彩色标签徽章（tags.color） |
| 指标区 | CPU/内存/磁盘三个**小圆环**（64px），圆环中心大号百分比，圆环下方小字标签 | RingProgress 组件，颜色随阈值渐变 |
| 流量区 | 实时上下行速度（↑↓图标+数值）+ **月流量进度条**（当月累计/配额，超 80% 变橙、100% 变红，无配额只显示数值） | 细进度条 |
| 延迟区 | **延迟小格子图**：默认展示 IPv4 全部线路（按 isp→region 自动分组命名，如"上海电信"），每行一个目标，展示最近 24 次探测的小格子；IPv6 目标在无 IPv6 出口时显示"无v6" | LatencyGrid 组件（见 6.4.5） |
| 尾部 | 到期时间（30天内黄/7天内红+⚠）+ 费用（币种符号+金额/周期） | 小字灰色 |

- 离线卡片：整体降低不透明度，圆环显示灰色 `---`，延迟格子全灰
- 卡片整体可点击进入详情页（尾部到期/费用区域可 hover 显示编辑入口）

#### 6.4.3 表格视图

紧凑列表，适合服务器多的场景（一屏看全所有机器）：

延迟/丢包列**按实际探测目标动态生成**（不硬编码"电信/联通/移动"，国际部署可自定义目标名）：

| 名称 | 状态 | 位置 | CPU | 内存 | 磁盘 | 上下行 | 月流量 | 电信 | 移动v6 | 到期 |
|------|------|------|-----|------|------|--------|--------|------|--------|------|
| 🇺🇸 US-LAX-01 | ● | 洛杉矶 | 45% | 60% | 70% | 1.2/3.5M | 320G/1T | 12ms | 155ms | 178天 |
| 🇭🇰 HK-家宽-02 | ● | 香港 | 12% | 30% | 50% | 0.5/1.0M | 110G/500G | 42ms | 无v6 | 6天⚠ |

- 延迟列 = 启用的探测目标（列头显示 target name），目标多时表格横向滚动；IPv6 目标带 `v6` 徽标
- CPU/内存/磁盘列用迷你进度条（单元格内 60px 宽）
- 延迟列显示当前平均延迟（带颜色分级小圆点），Agent 无 IPv6 出口显示"无v6"
- 行点击进详情，表头点击排序

#### 6.4.4 地图视图

```
┌──────────────────────────────────────────────────────────┐
│           (ECharts 世界地图, 深色底图)                     │
│                                                          │
│      ● ◉◉ 北美西海岸(2)                                  │
│              ● 日本(1)     ◉ 香港(3)                     │
│         ● ◉ 欧洲(2)          ◉ 新加坡(1)                 │
│                                                          │
│  ◉ = 该地区有多台服务器(数字为数量)                       │
│  光点带涟漪动画(effectScatter), 颜色=在线状态              │
│  点击光点 → 弹出该地区服务器小卡片列表 → 点击进详情        │
└──────────────────────────────────────────────────────────┘
```

- ECharts `map` + `effectScatter`，世界 GeoJSON 内嵌前端资源（不走 CDN）
- 光点坐标来自 GeoIP 解析（见 5.7），在线绿色/离线灰色
- 同一地区多台服务器聚合为一个光点，悬停显示数量，点击展开列表
- 地图下方保留概览条

#### 6.4.5 延迟小格子组件（LatencyGrid，核心组件）

```
单个格子: 颜色 = 延迟分级, 高度 = 延迟数值映射

 上海电信   ▁▂▁▁▂▁▁▁▂▁▁▁▁▂▁▁▁▁▁▂▁▁      当前: 12.5ms 丢包0%
 上海联通   ▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁      当前:  8.3ms 丢包0%
 上海移动   ▂▄▂▂▄▂▂▂▄▂▂▂▂▄▂▂▂▂▂▄▂▂      当前: 15.2ms 丢包10%

格子颜色分级 (柔和渐变, 不刺眼):
  <50ms   → #4ade80 (绿)
  <100ms  → #a3e635 (黄绿)
  <200ms  → #facc15 (黄)
  <400ms  → #fb923c (橙)
  ≥400ms  → #f87171 (红)
  超时/失败 → #9ca3af (灰)
格子高度: 按该行数据归一化(最小格子4px, 最高20px), 延迟越高格子越高
```

- 每行 = 一个探测目标（按 isp→region 分组命名），行内格子 = 最近 N 次探测（默认 24 次，即 24 分钟）
- 悬停单个格子：tooltip 显示该次探测的「时间 · 平均/最小/最大/抖动 · 丢包率」
- 行尾显示当前值（平均延迟 + 丢包率）
- 数据源：环形缓冲中的 Ping 历史（每 60 秒一个点，保留最近 1 小时 60 点）
- 新格子从右侧推入时带 300ms 平滑动画

### 6.5 服务器详情（Server Detail）

```
┌──────────────────────────────────────────────────────────────┐
│ ← 返回 | 🇺🇸 US-LAX-01 [生产][中转] ● 在线 | Linux x86_64 | 3天│
│    洛杉矶 · Bandwagon · IPv4: 1.2.3.4 · IPv6: 2600:...       │
├──────────────────────────────────────────────────────────────┤
│  时间范围: [1小时] [6小时] [12小时] [1天] [2天]               │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐ │
│  │ 延迟小格子图 (IPv4)                                     │ │
│  │  上海电信 ▁▂▁▁▂▁▁▁▂▁▁▁▁▂▁▁▁▁▁▂▁▁ (每行一条线路)       │ │
│  │  上海联通 ▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁                       │ │
│  │  上海移动 ▂▄▂▂▄▂▂▂▄▂▂▂▂▄▂▂▂▂▂▄▂▂                       │ │
│  ├────────────────────────────────────────────────────────┤ │
│  │ 延迟小格子图 (IPv6, 如有v6目标)                         │ │
│  │  上海电信v6 ▁▁▂▁▁▁▂▁▁▁▂▁▁▂▁▁▁▂▁▁▁                      │ │
│  └────────────────────────────────────────────────────────┘ │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐ │
│  │ CPU 占用率 (实时折线图)                                  │ │
│  │  100%┤                                                   │ │
│  │   80%┤    ╱╲     ╱╲                                      │ │
│  │   60%┤╱     ╲ ╱  ╲  ╱                                   │ │
│  │   40%┤                                                   │ │
│  │    0%└───────────────────────────────────────────────    │ │
│  │      当前: 45% | 平均: 42% | 最大: 78%                    │ │
│  │      负载: 0.52 / 0.48 / 0.50 (1/5/15分钟)                │ │
│  └────────────────────────────────────────────────────────┘ │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐ │
│  │ 内存占用率 (实时折线图)                                  │ │
│  │      当前: 60% | 总量: 8GB | Swap: 0%                     │ │
│  └────────────────────────────────────────────────────────┘ │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐ │
│  │ 延迟与丢包率融合图 (实时折线图)                           │ │
│  │  延迟: 三网三条折线 ── 电信  ── 联通  ── 移动            │ │
│  │  丢包率: 三网三色柱状(与延迟图共享X轴对齐)               │ │
│  │  当前: 电信 12.5ms/0% | 联通 8.3ms/0% | 移动 15.2ms/10%  │ │
│  └────────────────────────────────────────────────────────┘ │
│                                                              │
│  ┌──────────────┐ ┌──────────────┐ ┌──────────────┐        │
│  │ 磁盘使用率    │ │ 网络流量      │ │ 月流量统计    │        │
│  │ /: 70%      │ │ ↑1.2MB/s     │ │ 本月: 320GB  │        │
│  │ /data: 50%  │ │ ↓3.5MB/s     │ │ 配额: 1TB    │        │
│  │ (分区进度条) │ │ (实时图表)    │ │ [▓▓▓░░ 32%] │        │
│  └──────────────┘ └──────────────┘ └──────────────┘        │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐ │
│  │ 信息与费用                                              │ │
│  │ OS: Ubuntu 22.04 | 内核: 5.15.0-91 | CPU: Xeon 4核     │ │
│  │ Agent: v1.0.0 | 探测: ICMP | 上报: 3秒 | TCP:128 UDP:16│ │
│  │ 到期: 2027-01-15 | $49.99/年 | 历史流量: [月度柱状图]   │ │
│  └────────────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────────────┘
```

**详情页设计要点**：
- 顶部信息条：国旗 + 名称 + 标签 + 位置/供应商 + 出口 IPv4/IPv6（IPv6 折叠显示）
- 延迟格子图置顶（这是 VPS 用户最常看的数据，NodeGet 详情页的排布逻辑），IPv4/IPv6 分组展示，按「运营商 → 城市」自动分组命名
- 保留 CPU/内存实时折线图和延迟丢包融合图（时间范围 5 档切换）
- 月流量统计卡片：当月累计/配额进度条 + 最近 12 个月月度流量柱状图（历史归档数据）
- 信息与费用卡片：系统信息 + 到期/费用（管理员可点击编辑）

**时间范围切换数据源**：

| 时间范围 | 数据源 | 数据粒度 | 刷新方式 |
|---------|--------|---------|---------|
| 1 小时 | 环形缓冲 | 3 秒/点 | WebSocket 实时 |
| 6 小时 | 环形缓冲 + SQLite 聚合 | 3 秒/点 + 5 分钟/点 | WebSocket 实时 |
| 12 小时 | SQLite 聚合 | 5 分钟/点 | 定时轮询(5分钟) |
| 1 天 | SQLite 聚合 | 5 分钟/点 | 定时轮询(5分钟) |
| 2 天 | SQLite 聚合 | 5 分钟/点 | 定时轮询(5分钟) |

（延迟格子图固定使用环形缓冲最近 60 分钟数据，不随时间范围切换变化）

### 6.6 其他页面

**告警管理**：告警规则列表（CRUD，含流量配额/到期提醒规则）+ 告警历史记录（FIRING/RESOLVED 时间线）

**通知渠道**：Webhook/Telegram/邮件渠道管理，支持测试发送

**设置**：
- 账户安全（密码/TOTP/登录限速）
- 探测目标管理（目标列表 + region/isp/ip_version 元数据编辑）
- 服务器管理：Agent 列表（注册码/Token/一键安装命令）+ **元数据编辑**（显示名称/位置/国旗/标签/到期/费用/流量配额）
- 标签管理（创建/改名/颜色）
- GeoIP：显示 mmdb 库状态/版本/过期提示
- 系统设置（数据保留/Ping 间隔/静默期/汇率配置）
- 外观（主题/背景/卡片风格/强调色）
- 公开页配置（标题/Logo/页脚/包含的服务器/展示字段开关）

**公开分享页**（`/s/:share_id`，无需登录）：与仪表盘卡片视图同款 UI（NodeGet StatusShow 定位），但：
- 仅展示管理员勾选的 Agent，仅展示非敏感字段（无 IP 详情、无 Token、无配置）
- 可配置站点标题/Logo/页脚（存 share_pages 表）；Logo 建议本地上传，若填外链 URL 受 CSP `img-src` 约束（见 7.3 安全响应头）
- 支持卡片/表格/地图三视图与筛选（仅对公开的 Agent 生效）
- 无登录入口暴露（登录页路径不对外泄露，仅管理员知晓）
- 实时数据经 `GET /ws/public/:share_id` 推送（share_id 鉴权 + 白名单字段 + 同 share_id 最多 5 连接）；WebSocket 不可用时前端降级为 10 秒轮询 REST

### 6.7 内置主题系统

```
主题切换流程:
浏览器选择 ──▶ localStorage(xprobe.theme / xprobe.mode) ──▶ data-theme + data-mode ──▶ CSS 变量与组件样式

内置主题(随前端 bundle 编译, 不运行时加载):
├ mission: 任务控制 (新版, 深色技术)
├ editorial: 纸面编辑 (新版, 浅色编辑)
├ graphite: 石墨琥珀 (新版, 暖深色, 默认)
├ glass: 玻璃拟态 (经典)
├ classic: 经典实底 (经典)
├ minimal: 极简描边 (经典)
└ midnight: 深色高对比 (经典)

新版主题各有签名模式: 任务控制/石墨琥珀默认深色, 纸面编辑默认浅色; 切换主题时自动匹配, 之后仍可用明/暗按钮覆盖。主题由入口同步应用, 登录/初始化/公开页同样生效。

外观配置项(管理端 + 公开页各自独立):
├ 模式: 浅色 (Light) / 深色 (Dark) / 跟随系统 (System)  [默认: 跟随系统]
├ 背景: 纯色 / 内置渐变 / 本地上传图片(仅管理端)
├ 强调色: 预设8色 + 自定义色值
└ 圆环动画: 开/关(低性能设备友好)
```

安全约束：

- 不支持主题上传、主题市场、manifest 解析、预览抓取、主题更新、运行时 JS。
- 主题只能是 CSS 变量/静态资源；公开页不接受外链背景图，避免访客被第三方跟踪。
- 因此 CSP 不需要为第三方脚本开口子，也不需要独立主题域或 sandbox iframe。

### 6.8 实时数据 WebSocket 流

```
浏览器 ──▶ WebSocket连接 /ws/dashboard (管理会话认证 + Origin 校验)
                    │
                    ▼
            Server推送实时数据(每3秒):
            {
              "type": "dashboard_update",
              "servers": [
                {"id": 1, "online": true, "cpu": 45.2, "mem": 60.0,
                 "disk": 70.0, "rx": 1048576, "tx": 524288,
                 "ping": {"上海电信": 12.5, "上海联通": 8.3, ...},
                 "traffic_monthly": {"rx": 343597383680, "tx": 171798691840},
                 "expires_in_days": 178, ...},
                ...
              ]
            }
                    │
                    ▼
            前端Zustand store更新 ──▶ React组件重渲染 ──▶ 圆环/格子/图表更新
```

- 延迟格子的历史数据通过 REST API 初始化拉取（`GET /api/v1/servers/:id/history?range=1h`），之后由 WS 推送增量更新
- 断线重连后自动重新拉取全量数据（避免格子出现灰色空洞——NodeGet 主题踩过的坑）
- 页面从后台切回/窗口重新聚焦/网络恢复时，主动重新拉取一次全量数据

---

## 7. 安全设计

### 7.1 威胁模型

| 编号 | 威胁场景 | 攻击方式 | Nezha 是否中招 | 我们的防御 |
|------|---------|---------|---------------|-----------|
| T1 | 攻击者通过 Server 向 Agent 下发命令 | 利用 Cron 任务、终端会话、文件管理器等控制通道 | 是 | S1+S4：协议中无控制帧，代码中无执行功能 |
| T2 | 攻击者截获 Agent-Server 通信 | 同网段嗅探明文通信 | 是 | S2：公网强制 TLS；仅受信隧道回环段允许明文 |
| T3 | 攻击者利用多租户授权缺陷越权 | BOLA/IDOR 访问他人资源 | 是 | S5：第一版单管理员，无多租户攻击面 |
| T4 | 攻击者通过 Webhook 通知发起 SSRF | 通知 URL 指向内网，反射响应体 | 是 | S6：SSRF 防护层 |
| T5 | 攻击者暴力破解管理员密码 | 自动化登录尝试 | 通用威胁 | 登录限速 + 可选 TOTP |
| T6 | 攻击者窃取 Agent Token 后在其他机器使用 | 复制配置文件到另一台机器 | 通用威胁 | 主机指纹绑定 |
| T7 | 攻击者伪造 Agent 上报数据 | 用有效 Token 提交伪造数据 | 是 | 数据合理性校验 + 上报频率限制 |
| T8 | 攻击者通过 CSRF 触发状态变更 | 诱导已登录用户访问恶意链接 | 是 | 管理会话存于 HttpOnly Cookie + SameSite=Strict + Origin/Sec-Fetch-Site 校验 |
| T9 | 攻击者利用注册码重复注册 | 截获一键安装命令后重复使用 | 通用威胁 | 注册码一次性 + 15 分钟有效期 |
| T10 | Agent 以 root 运行导致 RCE 危害放大 | 即使不存在 RCE，root 运行增加风险 | 是 | S3：非 root + 最小 capabilities |
| T11 | 攻击者通过公开分享页泄露敏感信息 | 分享页暴露 IP/Token/配置 | 通用威胁 | 分享页白名单字段，仅展示非敏感信息 |
| T12 | 攻击者利用 WebSocket 端点未授权访问 | 直接连接 dashboard WS 获取所有数据 | 是 | WS 端点强制管理会话认证 + Origin 校验 |
| T13 | 攻击者攻破 Server 后通过探测目标配置驱使 Agent 发起任意 HTTP 请求（配置污染） | 篡改 ping_targets，把全部 Agent 变成内网/任意 URL 请求器（Nezha CVE-2026-47120 变体） | 是 | S9：HTTP Ping 固定形态（无 header/body 自由度、响应体不读取、目标数上限、限并发） |

### 7.2 S1 + S4：纯只读架构（根除 RCE）

**协议层**：WebSocket 消息协议中，Server→Agent 方向只有 1 种帧（heartbeat_ack 确认），不存在"命令"帧。

**代码层**：Agent 代码中不引入任何命令执行相关功能：
- 不调用 `os/exec`、`syscall.Exec`
- 不建立 PTY/Shell 会话
- 不提供文件系统操作接口
- 不包含 Cron 任务执行器

**架构层**：Server 到 Agent 方向不存在主动连接。所有通信由 Agent 发起。

**验证方法**：代码审计时搜索以下关键词，确保零匹配：
- `os/exec`、`exec.Command`、`exec.CommandContext`
- `pty`、`term`、`shell`
- `os.OpenFile`（写模式）、`os.Remove`、`os.Rename`
- `net.Dial`（Server 主动连接 Agent 的方向）

### 7.3 S5：单管理员 + 强认证

**管理会话安全配置**：

| 配置项 | 值 | 理由 |
|--------|-----|------|
| 令牌 | 256-bit 随机不透明令牌 | 不是 JWT；没有签名密钥可被文件读取漏洞偷走 |
| 服务端存储 | 只存 SHA-256(token) | 数据库/备份泄露不能直接伪造会话 |
| Cookie | HttpOnly + Secure + SameSite=Strict + Path=/ | 防 JS 读取、防跨站提交、仅 HTTPS |
| 有效期 | 2 小时空闲过期 + 7 天绝对过期 | 短窗口 + 服务端可立即吊销 |
| 会话表 | id / token_hash / admin_id / expires_at / idle_expires_at / revoked / last_seen / ip / user_agent | 支持会话列表、单条吊销、改密全量吊销 |
| 登录限速 | 5 次/分钟/IP；连续 10 次失败锁定 15 分钟 | 防密码爆破与 Argon2 资源耗尽 |
| 续期 | 仅活跃请求刷新 idle_expires_at，不延长绝对过期 | 避免无限续期 |

**密码策略**：
- 首次启动强制设置密码（无默认密码，不像 Nezha 默认 admin/admin）
- 最小长度 12 位
- 必须包含大小写字母 + 数字
- Argon2id（内存 64 MiB、迭代 3 次、并行度 2），替代 bcrypt
- 密码哈希与 TOTP 密钥都不进入日志/API 响应

**首次初始化流程（无默认密码）**：
1. Server 首次启动检测到 admin 表为空，生成一次性 setup token（256-bit 随机，Base64URL 展示，30 分钟有效；过期后重启生成新 token）
2. setup token 打印到控制台/日志，同时写入数据目录 `setup_token` 文件（权限 600）
3. 访问 `/setup`，输入 setup token 并设置管理员密码（最小 12 位，含大小写与数字）+ 可选 TOTP
4. 提交成功后 token 立即作废；admin 表非空后 `/setup` 永久返回 404
5. 遗失 token：`probe-server setup-token --reset` 在本机重新生成（本机 shell 权限本身即是授权凭证）

**TOTP 恢复**：启用 TOTP 时生成 10 个一次性恢复码（只存 SHA256 哈希，用后即删）；密码与恢复码同时丢失时用 `probe-server reset-admin` 在本机重置。

**安全响应头**（所有 HTTP 响应，含公开分享页，中间件统一注入）：

| Header | 值 | 说明 |
|--------|-----|------|
| Content-Security-Policy | `default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self' wss:; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'` | XSS 纵深防御；内置主题不需要第三方脚本；公开页不接受外链图片/Logo |
| Strict-Transport-Security | `max-age=31536000; includeSubDomains` | 强制 HTTPS |
| X-Frame-Options | `DENY` | 防点击劫持 |
| Referrer-Policy | `no-referrer` | 不泄露路径 |
| X-Content-Type-Options | `nosniff` | 禁止 MIME 嗅探 |

### 7.4 S6：SSRF 防护实现

```go
// 伪代码: SSRF防护检查流程
func safeHTTPSend(url string) error {
    // 1. 解析URL
    parsed := net.ParseURL(url)
    if parsed.Scheme != "https" && parsed.Scheme != "http" {
        return errors.New("only http/https allowed")
    }

    // 2. 解析所有IP地址
    ips, _ := net.LookupIP(parsed.Hostname())
    for _, ip := range ips {
        if isPrivateIP(ip) {
            return errors.New("private IP blocked")
        }
    }

    // 3. 自定义Dialer, 强制连接到解析的IP(防DNS重绑定)
    dialer := &net.Dialer{Timeout: 10 * time.Second}
    transport := &http.Transport{
        DialContext: func(ctx, network, addr) (net.Conn, error) {
            host, port, _ := net.SplitHostPort(addr)
            if host == parsed.Hostname() {
                addr = net.JoinHostPort(ips[0].String(), port)
            }
            return dialer.DialContext(ctx, network, addr)
        },
    }

    // 4. 禁止重定向到内网
    client := &http.Client{
        Timeout: 10 * time.Second,
        Transport: transport,
        CheckRedirect: func(req *http.Request, via []*http.Request) error {
            return checkSSRF(req.URL.String())
        },
    }
    resp, _ := client.Get(url)
    defer resp.Body.Close()

    // 5. 限制响应体读取(最多1KB)
    body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
    return nil
}
```

### 7.5 主机指纹绑定（防 Token 盗用）

```go
// 指纹 = SHA256(安装盐 + CPU型号 + 主网卡MAC + 系统类型)
// 不依赖 hostname(易变, 用户改hostname会误拒连); 安装盐由 install.sh 生成并持久化于 Agent 配置
func generateHostFingerprint(salt string) string {
    cpuModel := readCPUModel()
    macAddr := getPrimaryMAC()
    raw := salt + "|" + cpuModel + "|" + macAddr + "|" + runtime.GOOS
    h := sha256.Sum256([]byte(raw))
    return hex.EncodeToString(h[:])
}
```

- 注册时上报主机指纹，Server 绑定 Token 与指纹（UNIQUE 约束：同指纹仅允许一台在线）
- 后续上报时 Server 校验指纹，不匹配则拒绝并记录安全日志
- 指纹变更（更换网卡/重装系统）→ 面板调用 `POST /api/v1/servers/:id/reset-fingerprint` 重置，Agent 下次注册时重新绑定
- 冲突处理：注册时指纹已存在（同模板批量 VPS 的极端撞车）→ 返回 409 指纹冲突，管理员选择「删除旧记录」或「重置旧记录指纹」后重试
- 防护边界（诚实声明）：指纹防的是「Token 从网络侧泄露后被其他机器冒用」；若攻击者已读取本机配置文件（Token 与安装盐都在其中），指纹无法提供保护——该场景属于主机已被完全控制，超出本方案威胁模型

### 7.6 数据合理性校验（防伪造上报）

| 校验项 | 规则 | 异常处理 |
|--------|------|---------|
| CPU 使用率 | 0-100% | 超范围丢弃，记录告警 |
| 内存使用率 | 0-100%，used ≤ total | 超范围丢弃 |
| 磁盘使用率 | 0-100% | 超范围丢弃 |
| 延迟 | 0-60000ms | 超范围丢弃 |
| 丢包率 | 0-100% | 超范围丢弃 |
| 上报频率 | 每 3 秒 ±1 秒 | 过快拒绝（防刷），过慢标记离线 |
| 数据大小 | 单次上报 ≤ 10KB | 超大拒绝 |
| 数组长度 | disk ≤ 32 项, ping_result ≤ 64 项 | 超限截断并记录安全日志 |
| 字符串/格式 | hostname ≤ 128 字符; ipv4/ipv6 须通过 net.ParseIP 校验 | 非法丢弃 |

### 7.7 安全审计清单

开发完成后，按此清单审计：

- [ ] 全局搜索 `os/exec`、`exec.Command`，确认零匹配（S4）
- [ ] 全局搜索 `pty`、`shell`、`terminal`，确认 Agent 侧零匹配（S4）
- [ ] 确认所有 WebSocket 帧类型有明确定义，无"通用命令"帧（S1）
- [ ] 确认 TLS 配置不可关闭，Agent 拒绝明文连接（S2）
- [ ] 确认会话 Cookie 为 HttpOnly + Secure + SameSite=Strict，且服务端只存 token_hash（S5）
- [ ] 确认代码库中不存在 jwt_secret / JWT 签发逻辑
- [ ] 确认 agents.token_hash、register_codes.code_hash 不含明文
- [ ] 确认 Cloudflare/受信代理模式下只信任回环或已配置 CIDR 的 CF 头
- [ ] 确认公开页无第三方 JS、无外链背景图（仅内置主题）
- [ ] 确认登录限速生效（S5）
- [ ] 确认 Webhook 通知经过 SSRF 防护（S6）
- [ ] 确认 Agent systemd service 以 probe 用户运行（S3）
- [ ] 确认配置文件权限 600（S8）
- [ ] 确认注册码一次性 + 15 分钟有效期（T9）
- [ ] 确认主机指纹校验生效（T6）
- [ ] 确认数据合理性校验生效（T7）
- [ ] 确认公开分享页不包含敏感信息（T11）
- [ ] 确认所有 WebSocket 端点需要认证（T12）
- [ ] 确认 HTTP Ping 无自定义 header/body 代码路径，响应体不读取（S9）
- [ ] 确认 register 端点限速生效（T9 配套）
- [ ] 确认安全响应头 CSP/HSTS/X-Frame-Options 生效
- [ ] 确认 systemd unit 使用 AmbientCapabilities 且 NoNewPrivileges=true 下 ICMP 仍可用
- [ ] 确认 setup token 一次性消费，admin 表非空后 /setup 返回 404

---

## 8. 部署与运维

### 8.1 Server 部署方式

#### Docker 部署（推荐）

```yaml
version: '3.8'
services:
  probe-server:
    image: probe-server:latest
    container_name: probe-server
    restart: unless-stopped
    ports:
      - "443:443"
    volumes:
      - ./data:/app/data
      - ./certs:/app/certs
    environment:
      - PROBE_ADMIN_USER=admin
      - PROBE_DATA_RETENTION=90
    read_only: true
    tmpfs:
      - /tmp
    security_opt:
      - no-new-privileges:true
    cap_drop:
      - ALL
```

> Cloudflare Tunnel 模式：删除 `ports` 映射，Server 只监听 `127.0.0.1:8080`，由同机 `cloudflared` 连接；不要让容器直接暴露公网明文端口。

#### 二进制部署

```bash
curl -fsSL https://your-server.com/install-server.sh | bash
```

#### Cloudflare 入口（可选，两种模式）

**模式 A：Cloudflare Tunnel（推荐）**

```
Agent/浏览器 ──wss/https──▶ Cloudflare 边缘 ──隧道──▶ cloudflared ──http──▶ 127.0.0.1:8080
```

- Server 绑定 `127.0.0.1:8080`，不开放公网端口；`tls.mode: off` 只在这种回环场景允许。
- Cloudflare 终结公网 TLS；Agent 仍连接 `wss://monitor.example.com`，从 Agent 视角仍是强制 TLS。
- 只有来自回环地址的请求才信任 `CF-Connecting-IP` / `CF-Visitor`；用 `CF-Connecting-IP` 做限速，不信任客户端自带的 `X-Forwarded-For`。
- `CF-Visitor: https` 或 `X-Forwarded-Proto: https` 时设置 Secure Cookie。
- 管理端可挂 Cloudflare Access；Agent 注册/WS 路径必须 bypass。
- 免费计划支持 WebSocket；3 秒上报 + 30 秒心跳足够保活，另加 Ping 帧与指数退避重连。

**模式 B：橙云代理 + Origin Certificate**

- 源站用 Cloudflare Origin Certificate 提供 HTTPS，SSL 模式 Full (strict)。
- 开启 Authenticated Origin Pulls，防火墙只允许 Cloudflare IP 段访问源站。
- 应用仍强制 TLS；受信代理需配置 Cloudflare CIDR，并定期维护 IP 段。
- 不 Pin Cloudflare 叶子证书；使用系统 CA，或 Pin CA 并准备轮换方案。

**不支持：Workers 运行时**

Go 二进制、SQLite 和 Agent 长连接 WebSocket 都不适合 Workers。若未来需要“纯 Cloudflare”版本，应作为独立的 TypeScript + D1/Durable Objects 边缘转发器，而不是复制一套业务逻辑。

### 8.2 Server 配置文件

```yaml
listen: "127.0.0.1:8080"               # 直连公网部署改为 :443; Cloudflare Tunnel 模式保持回环
public_url: "https://monitor.example.com"
admin_url: "https://admin.example.com" # 可选: 与公开页分域, 便于挂 Cloudflare Access
data_dir: "/var/lib/probe-server"

tls:
  mode: "files"                        # files|off; off 只允许绑定回环地址
  cert: "/etc/probe-server/cert.pem"
  key: "/etc/probe-server/key.pem"

proxy:
  mode: "none"                         # none|cloudflare_tunnel|cloudflare_proxy
  trusted_cidrs: []                    # cloudflare_proxy 模式填 Cloudflare IP 段; tunnel 模式仅信任回环

auth:
  session_idle: "2h"                   # 空闲过期; 活跃请求只续 idle, 不延长绝对过期
  session_max: "168h"                  # 绝对过期(7d)
  login_rate_limit: 5                  # 次/分钟/IP; 连续10次失败锁定账号15分钟

monitor:
  report_interval: 3
  heartbeat_timeout: 90
  ring_buffer_size: 3600               # 3秒/点 × 3小时 = 3600点(见5.3)
  aggregation_interval: 300

storage:
  data_retention_days: 90
  sqlite_journal_mode: "WAL"           # 高频写(聚合落盘+告警状态迁移), WAL避免读写互斥
  sqlite_busy_timeout_ms: 5000

ping:
  default_interval: 60
  icmp_count: 10
  icmp_timeout: 15
  icmp_interval: 500
  tcp_count: 5
  tcp_timeout: 5

alert:
  default_silence_period: 3600

notify:
  webhook_timeout: 10
  webhook_max_response: 1024

log:
  level: "info"
  file: "/var/log/probe-server/server.log"
  max_size: 100
  max_backups: 5
  max_age: 30
```

### 8.3 Agent 一键安装脚本

```bash
#!/bin/bash
# install-agent.sh

# 1. 解析参数
SERVER_URL=""
REGISTER_CODE=""
while [[ $# -gt 0 ]]; do
    case $1 in
        --server) SERVER_URL="$2"; shift 2;;
        --code) REGISTER_CODE="$2"; shift 2;;
        *) echo "Unknown: $1"; exit 1;;
    esac
done

# 2. 检测系统
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case $ARCH in
    x86_64)  ARCH="amd64";;
    aarch64) ARCH="arm64";;
    armv7l)  ARCH="armv7";;
    *) echo "Unsupported: $ARCH"; exit 1;;
esac

# 3. 下载二进制 + 签名, 验签后再安装
AGENT_URL="${SERVER_URL}/download/agent/${OS}/${ARCH}"
curl -fsSL -o /tmp/probe-agent "${AGENT_URL}"
curl -fsSL -o /tmp/probe-agent.sig "${AGENT_URL}.sig"
# 公钥随安装脚本/发布页分发; 用 Ed25519 验签, 不通过则中止
probe-verify --pubkey "${AGENT_SIGNING_PUBKEY}" --file /tmp/probe-agent --sig /tmp/probe-agent.sig
# SHA-256 仅作传输完整性辅助, 不能替代签名
curl -fsSL -o /tmp/probe-agent.sha256 "${AGENT_URL}.sha256"
echo "$(cat /tmp/probe-agent.sha256)  /tmp/probe-agent" | sha256sum -c -

# 4. 安装二进制
chmod +x /tmp/probe-agent
mv /tmp/probe-agent /usr/local/bin/probe-agent

# 5. 创建用户
if ! id probe &>/dev/null; then
    useradd -r -s /usr/sbin/nologin probe
fi

# 6. 创建配置目录与状态目录, 写入配置
mkdir -p /etc/probe-agent /var/lib/probe-agent
cat > /etc/probe-agent/config.yml << EOF
server: "${SERVER_URL}"
register_code: "${REGISTER_CODE}"
install_salt: "$(openssl rand -hex 32 2>/dev/null || head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n')"
report_interval: 3
config_sync_interval: 3600
ping_method: "auto"
EOF
chmod 600 /etc/probe-agent/config.yml
chown -R probe:probe /etc/probe-agent /var/lib/probe-agent

# 6.1 仅直连自签模式: 指纹由管理员复制的一键命令带外传入
# 不要从 ${SERVER_URL}/api/v1/server-cert 现取: 同一条未认证连接取证书和指纹等于 TOFU
if [ -n "${CERT_FP}" ]; then
    printf 'tls_pin_mode: "spki_sha256"\nserver_cert_fingerprints: ["%s"]\n' "${CERT_FP}" >> /etc/probe-agent/config.yml
fi

# 6.2 尝试放开 unprivileged ICMP 内核参数(失败不阻塞, Agent 自动降级并如实标注)
sysctl -w net.ipv4.ping_group_range="0 2147483647" 2>/dev/null \
    || echo "WARN: ping_group_range 不可写, unprivileged ICMP 将不可用"

# 7. 尝试setcap (仅非 systemd 场景备用; systemd 场景由 unit 的 AmbientCapabilities 授予, 见4.8)
if setcap cap_net_raw+ep /usr/local/bin/probe-agent 2>/dev/null; then
    echo "setcap ok (for non-systemd use)"
else
    echo "setcap failed (non-fatal; systemd unit grants CAP_NET_RAW via AmbientCapabilities)"
fi

# 8. 安装systemd service
cat > /etc/systemd/system/probe-agent.service << 'EOF'
[Unit]
Description=Server Probe Agent
After=network.target

[Service]
Type=simple
User=probe
Group=probe
ExecStart=/usr/local/bin/probe-agent --config /etc/probe-agent/config.yml
Restart=always
RestartSec=5
# ICMP 能力由 systemd 授予(见4.8): setcap 与 NoNewPrivileges 互斥
AmbientCapabilities=CAP_NET_RAW
CapabilityBoundingSet=CAP_NET_RAW
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
PrivateDevices=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictAddressFamilies=AF_INET AF_INET6 AF_NETLINK AF_UNIX
RestrictSUIDSGID=true
MemoryMax=128M
ReadWritePaths=/etc/probe-agent /var/lib/probe-agent

[Install]
WantedBy=multi-user.target
EOF

# 9. 启动服务
systemctl daemon-reload
systemctl enable probe-agent
systemctl start probe-agent

echo "Agent installed and started."
echo "Check status: systemctl status probe-agent"
```

### 8.4 版本发布流程

```
1. 更新版本号 (VERSION文件)
2. 构建前端 (cd frontend && npm run build → web/)
3. 交叉编译:
   Server: linux-amd64, linux-arm64
   Agent: linux-amd64, linux-arm64, linux-armv7 (v1 仅Linux, 见4.1平台范围; windows/darwin为v2计划)
4. 生成SHA256校验文件 + Ed25519 签名(.sig)，公钥随发布页/安装脚本公布
5. 构建Docker镜像 (linux/amd64, linux/arm64)
6. 发布GitHub Release
```

### 8.5 升级方案

**Server 升级**：停止服务 → 替换二进制 → 启动服务。Schema 变更使用**仓库自带的 embed 顺序迁移器**（`server/internal/repository/migrate.go`，迁移 SQL 随二进制 embed，启动时按版本号顺序执行并记录到 schema_migrations 表）；**禁用 GORM AutoMigrate**。

**Agent 升级**：手动升级（第一版不做自动升级，因为自动升级违反纯只读架构）

**注意**：Agent 自动升级意味着 Server 能向 Agent 下发新二进制并执行，这违反了纯只读架构（S1+S4）。第一版只做手动升级，面板提示哪些 Agent 需要升级。Server 向下兼容旧版 Agent 的上报格式。

### 8.6 备份与恢复

```bash
# 备份(在线一致性快照; 直接 cp 运行中的 WAL 库会得到不一致副本)
sqlite3 /var/lib/probe-server/probe.db ".backup '/backup/probe-$(date +%Y%m%d).db'"

# 恢复
systemctl stop probe-server
cp /backup/probe-20260621.db /var/lib/probe-server/probe.db
rm -f /var/lib/probe-server/probe.db-wal /var/lib/probe-server/probe.db-shm
systemctl start probe-server
```

### 8.7 日志设计

```
日志级别: DEBUG / INFO / WARN / ERROR

日志格式 (JSON):
{"time":"2026-06-21T10:00:00Z","level":"INFO","module":"monitor","msg":"agent connected","agent_id":1}
{"time":"2026-06-21T10:05:00Z","level":"WARN","module":"monitor","msg":"agent heartbeat timeout","agent_id":3}
{"time":"2026-06-21T10:10:00Z","level":"ERROR","module":"notify","msg":"webhook send failed","error":"timeout"}

安全相关日志 (独立记录):
- 登录失败/限速
- 主机指纹不匹配
- SSRF 阻断
- 注册码使用
```

### 8.8 监控自身健康

Server 面板"系统状态"页面（仅管理员）：

| 指标 | 说明 |
|------|------|
| Server 运行时间 | 当前进程运行时长 |
| 内存占用 | Server 自身内存使用 |
| SQLite 大小 | 数据库文件大小 |
| 在线 Agent 数 | 当前连接的 Agent 数量 |
| WebSocket 连接数 | 浏览器面板连接数 |
| 上报 QPS | 每秒接收的 Agent 上报数 |
| 磁盘剩余空间 | 数据目录所在分区剩余空间 |

---

## 9. 开发计划与里程碑

### 9.1 里程碑规划

基于 TDD（测试驱动开发）原则，每个里程碑都遵循"先写测试 → 实现 → 测试通过"的循环。

```
M1: 基础架构 + 核心采集 (2周)
    │
    ▼
M2: Agent-Server 通信 + 注册上线 (2周)
    │
    ▼
M3: 实时监控 + 前端面板·NodeGet 风格 (4周)
    │
    ▼
M4: 网络探测(Ping) + 历史数据 (2周)
    │
    ▼
M5: 告警 + 通知 + 安全加固 (2周)
    │
    ▼
M6: 部署 + 发布 + 文档 (1周)
```

### 9.2 各里程碑详细任务

#### M1: 基础架构 + 核心采集（2 周）

**目标**：Agent 能采集系统指标，Server 能存储数据。

| 任务 | 类型 | 说明 |
|------|------|------|
| 项目初始化 | 代码 | Go module 初始化，目录结构搭建，共享 model 包 |
| 采集器：CPU | TDD | 读 `/proc/stat`，计算使用率，写测试验证计算逻辑 |
| 采集器：内存 | TDD | 读 `/proc/meminfo`，解析 MemTotal/MemAvailable |
| 采集器：磁盘 | TDD | 系统调用 statfs，遍历挂载点 |
| 采集器：网络 | TDD | 读 `/proc/net/dev`，计算速率差值 |
| 采集器：系统信息 | TDD | uname 系统调用，运行时间，进程数 |
| SQLite 数据层 | TDD | 内嵌顺序迁移 + 原生 SQL + repository CRUD 测试（禁用 GORM AutoMigrate） |
| 环形缓冲 | TDD | 写入/读取/覆盖逻辑，并发安全测试 |

**验收标准**：
- Agent 能采集 CPU/内存/磁盘/网络/系统信息并输出到 stdout（JSON）
- Server 能初始化 SQLite 并通过 repository 层 CRUD
- 环形缓冲单元测试覆盖率 ≥ 90%

#### M2: Agent-Server 通信 + 注册上线（2 周）

**目标**：Agent 能注册到 Server 并维持 WebSocket 连接，数据能上报。

| 任务 | 类型 | 说明 |
|------|------|------|
| WebSocket 服务端 | TDD | Server 端 WS 端点，连接管理，消息分发 |
| WebSocket 客户端 | TDD | Agent 端 WS 客户端，自动重连，心跳维持 |
| 注册流程 | TDD | 注册码/Token 均哈希存储；一次性消费；签发时只返回一次明文 Token |
| 主机指纹 | TDD | 指纹生成，注册时绑定，上报时校验 |
| 管理会话 | TDD | 不透明令牌 + token_hash；Argon2id；Cookie 安全标志；登录限速 |
| TLS/Cloudflare 入口 | 代码 | `tls.mode`；Tunnel/橙云；只信任回环或配置 CIDR 的 CF 头；Secure Cookie 判定 |
| Agent 二进制签名 | 代码 | Ed25519 签名发布；安装/升级前验签 |
| 数据上报协议 | TDD | report/heartbeat 消息的序列化/反序列化 |
| 配置拉取 | TDD | Agent 定时 GET 拉取探测目标配置 |
| 数据合理性校验 | TDD | 上报数据范围校验，频率限制 |
| 注册限速 + setup token | TDD | register 限速 5 次/分钟/IP；首启签发一次性 setup token（30 分钟有效） |
| 安全回归测试 | TDD | Token 重放、WS Origin、路径穿越、CF 头伪造、Cookie 标志、注册越权 |

**验收标准**：
- Agent 能通过注册码注册并获取 Token
- Agent 能维持 WebSocket 长连接，3 秒上报一次数据
- 注册码一次性消费，15 分钟过期
- 公网全程 TLS；回环明文仅允许在 Cloudflare Tunnel 模式下出现
- Agent Token、注册码、管理会话令牌在数据库中均无明文
- 注册端点限速生效（5 次/分钟/IP）
- 首启 setup token 一次性，admin 初始化完成后 /setup 关闭
- 断线自动重连（指数退避：1s→2s→4s→...→60s 上限）

#### M3: 实时监控 + 前端面板（4 周）

**目标**：浏览器面板以 NodeGet 风格实时展示服务器状态（延迟数据在 M4 接入前用占位/mock）。

| 任务 | 类型 | 说明 |
|------|------|------|
| 前端项目初始化 | 代码 | React + Vite + shadcn/ui + Tailwind + ECharts + Zustand |
| 设计系统基础组件 | TDD | 玻璃拟态卡片、RingProgress 圆环、延迟色阶常量、LatencyGrid 延迟格子组件（组件测试用 mock 数据驱动，真实数据 M4 接入） |
| 登录页 | TDD | 密码登录，限速，不透明会话 Cookie（HttpOnly/Secure/SameSite=Strict） |
| WebSocket 实时推送 | TDD | Server→浏览器 WS 推送，前端 Zustand store，断线重连/页面可见性恢复 |
| 仪表盘卡片视图 | 代码 | 玻璃拟态卡片：国旗+标签徽章、三圆环指标、实时上下行速度、月流量进度条、到期/费用，离线卡片整体降透明度 |
| 仪表盘表格视图 | 代码 | 紧凑表格 + 迷你进度条 + 概览条（延迟列 M4 接入前显示占位） |
| 筛选栏 | 代码 | 搜索/标签/地区/IPv4/IPv6/排序组合筛选，URL 参数持久化 |
| GeoIP 解析 | TDD | 离线 mmdb 查询（见 5.7），Agent 公网 IP → 坐标/城市/国家码，随 API 下发 |
| 仪表盘地图视图 | 代码 | ECharts 世界地图 + effectScatter 涟漪光点 + 同地区多机聚合 + 点击弹出服务器列表 |
| 服务器详情页 | 代码 | 延迟格子图置顶（mock 数据）+ CPU/内存实时折线 + 磁盘/网络/月流量卡片 + 信息与费用卡片 |
| 时间范围切换 | 代码 | 1H/6H/12H/1D/2D，混合数据源拼接 |
| 元数据编辑 + 标签管理 | TDD | 设置页：显示名称/位置/到期/费用/流量配额编辑，标签 CRUD（彩色徽章） |
| 内置主题 | 代码 | 4 套内置 CSS 主题 + 浅色/深色/跟随系统 + 内置渐变/本地背景；无第三方主题/运行时 JS |
| Go embed 前端 | 代码 | 前端构建产物内嵌到 Server 二进制 |
| setup 初始化页 | 代码 | /setup：输入一次性 token 设置管理员密码（+TOTP），admin 已存在时 404 |
| 面板 WS 认证 | TDD | 管理会话认证 + Origin 校验 |

**验收标准**：
- 仪表盘卡片/表格/地图三视图切换正常
- 卡片含圆环指标、月流量进度条、到期/费用展示，离线卡片降透明度
- 筛选组合生效且 URL 参数持久化
- 地图按 GeoIP 坐标定位光点，同地区多机聚合，点击可展开列表
- 详情页 CPU/内存折线图实时刷新，延迟格子组件单测通过（真实数据 M4 接入）
- 服务器元数据与标签可在设置页编辑管理
- 主题与背景切换正常，且全部为内置 CSS 主题（运行时无第三方 JS/外链图片）
- 5 档时间范围切换正常，1H/6H 实时刷新
- 单二进制部署（前端内嵌）

#### M4: 网络探测(Ping) + 历史数据（2 周）

**目标**：Agent 执行三网 Ping 探测，历史数据落盘和查询。

| 任务 | 类型 | 说明 |
|------|------|------|
| ICMP Ping 采集器 | TDD | pro-bing privileged 模式，10 包采样，完整统计 |
| Unprivileged ICMP | TDD | 降级方案，Linux SOCK_DGRAM |
| TCP Ping 采集器 | TDD | 5 次采样，降级方案 |
| HTTP Ping 采集器 | TDD | 3 次采样，排除 DNS 时间 |
| Ping 方法自动选择 | TDD | privileged→unprivileged→TCP 降级逻辑 |
| 探测目标配置同步 | TDD | Agent 拉取目标列表，本地缓存 |
| 数据聚合落盘 | TDD | 每 5 分钟聚合实时数据写入 SQLite |
| 历史数据查询 API | TDD | 按时间范围查询聚合数据 |
| 历史数据清理 | TDD | 定时清理超期数据（默认 90 天） |
| 延迟格子数据接入 | 代码 | 卡片/表格/详情页延迟格子接入真实 Ping 历史（环形缓冲最近 60 分钟），行尾显示当前延迟+丢包率 |
| 延迟丢包融合图 | 代码 | 详情页三网延迟折线 + 丢包柱状图共享 X 轴 |
| 探测并发调度 | TDD | 目标间并发（全局 semaphore 默认 8）；目标数上限 ≤64（HTTP ≤32） |

**验收标准**：
- ICMP Ping 10 包采样，报告平均/最小/最大/抖动/丢包率
- Ping 方法自动降级正常
- 历史数据每 5 分钟落盘，查询 API 正常
- 超期数据自动清理
- 仪表盘卡片/表格/详情页延迟格子展示真实探测数据，悬停显示该次完整统计

#### M5: 告警 + 通知 + 安全加固（2 周）

**目标**：告警引擎、通知渠道、安全审计全部就绪。

| 任务 | 类型 | 说明 |
|------|------|------|
| 告警规则 CRUD | TDD | API + 前端管理页 |
| 告警状态机 | TDD | OK→PENDING→FIRING→RESOLVED |
| 告警作用域 + 多渠道 | TDD | scope 匹配（all/agent_ids/tag_ids）+ notify_channel_ids |
| 通知去重 + 静默期 | TDD | 同一告警静默期内不重复 |
| 固定通知模板 | TDD | 变量替换 hostname/rule_name/metric/value/time |
| Webhook 通知 + SSRF 防护 | TDD | 内网过滤/重定向检测/响应体限制 |
| Telegram 通知 | TDD | Bot API 发送 |
| 邮件通知 | TDD | SMTP 发送 |
| TOTP 两步验证 | TDD | TOTP 密钥生成/验证 |
| TOTP 恢复码 | TDD | 10 个一次性恢复码（存哈希），用后即删 |
| 公开分享页 | 代码 | 与仪表盘同款三视图 UI，白名单字段，无敏感信息，站点标题/Logo/页脚可配置 |
| 公开页实时 WS | TDD | /ws/public/:share_id 白名单字段推送，连接数限制 |
| 安全响应头 | 代码 | CSP/HSTS/X-Frame-Options/Referrer-Policy 中间件 |
| 安全审计 | 代码 | 按 7.7 清单逐项审计（含 S9/限速/CSP/AmbientCapabilities/setup） |

**验收标准**：
- 告警规则能正确触发和恢复，作用域匹配正确（all/agent_ids/tag_ids），多渠道通知生效
- Webhook 通知经过 SSRF 防护，内网地址被拒绝
- 通知去重和静默期生效，通知内容含模板变量替换
- TOTP 两步验证可用，恢复码可用
- 公开页 WS 实时推送正常，安全响应头生效
- 安全审计清单全部通过

#### M6: 部署 + 发布 + 文档（1 周）

**目标**：可发布的生产版本。

| 任务 | 类型 | 说明 |
|------|------|------|
| Docker 镜像构建 | 代码 | 多阶段构建，多架构 |
| Server 一键安装脚本 | 代码 | 检测架构/下载/配置/systemd |
| Agent 一键安装脚本 | 代码 | 含注册码/创建用户/setcap/systemd |
| Agent 升级脚本 | 代码 | 手动升级流程 |
| GitHub Release | 代码 | CI/CD 自动构建发布 |
| 用户文档 | 文档 | 安装/配置/使用/FAQ |
| 安全文档 | 文档 | SECURITY.md，安全报告流程 |

**验收标准**：
- Docker 部署成功
- 一键安装脚本在 Ubuntu/Debian/CentOS 上测试通过
- GitHub Release 包含 linux/amd64 与 linux/arm64 二进制、SHA256 和 Ed25519 签名（.sig）
- 文档覆盖安装、配置、使用

### 9.3 总工期与优先级

| 里程碑 | 工期 | 累计 | 优先级 |
|--------|------|------|--------|
| M1 基础架构 + 核心采集 | 2 周 | 2 周 | P0 |
| M2 通信 + 注册上线 | 2 周 | 4 周 | P0 |
| M3 实时监控 + 前端面板 | 4 周 | 8 周 | P0 |
| M4 网络探测 + 历史数据 | 2 周 | 10 周 | P0 |
| M5 告警 + 通知 + 安全加固 | 2 周 | 12 周 | P1 |
| M6 部署 + 发布 + 文档 | 1 周 | 13 周 | P1 |

**总计约 13 周（3 个月）**，M1-M4 是最小可用版本（MVP），M5-M6 是完整版本。M3 因 NodeGet 风格 UI 改版（三视图/地图/GeoIP/元数据管理）由 3 周扩至 4 周。

### 9.4 技术风险与缓解

| 风险 | 影响 | 缓解措施 |
|------|------|---------|
| pro-bing 库在特定平台行为不一致 | Ping 数据不准 | M4 阶段在多平台测试，记录差异 |
| 环形缓冲并发写入竞争 | 数据丢失或 panic | M1 阶段充分测试并发安全，用 sync.Mutex 或 channel |
| SQLite 高频写入性能 | Server 卡顿 | 环形缓冲吸收高频写入，SQLite 只存聚合数据 |
| GeoIP 离线库缺失或定位偏差 | 地图视图不可用或位置不准 | 无 mmdb 时地图视图优雅降级提示，卡片/表格不受影响；管理员手动设置 region/country_code 覆盖自动解析 |
| 前端 embed 后调试困难 | 开发效率低 | 开发模式用独立前端 dev server，构建时才 embed |
| WebSocket 断线重连风暴 | Server 连接数飙升 | 指数退避重连（1s→2s→4s→...→60s 上限） |
| unprivileged ICMP 依赖 ping_group_range sysctl | 部分 Linux 环境降级 TCP Ping | 安装脚本尝试放开并如实标注 ping_method，不做静默降级 |

### 9.5 后续规划备忘（不在当前版本范围）

- 断线重连 ping 补传：Agent 本地缓存最近 10 分钟 ping 结果，重连后补发 `ping_result_backfill` 帧（协议预留，v1 不实现）
- 服务可用率页：HTTP Ping 聚合 30 天可用率 + 事件时间线（对标 Nezha Services）
- 自定义通知模板、全站公开模式开关、前端 i18n
- Windows/macOS Agent（v2，见 4.1）

---

## 10. AI 开发提示词

以下提示词可直接用于 AI 编程工具（如 TRAE、Cursor、Claude）进行开发。按里程碑顺序使用，每个里程碑完成后进入下一个。

### 10.1 通用上下文提示词（每次对话开始时使用）

```
你正在开发一款安全优先的服务器探针系统（v1 仅支持 Linux：Server 与 Agent 均只构建 linux/amd64 与 linux/arm64，Windows/macOS 留待 v2）。请遵循以下核心安全原则（不可妥协）：

1. 纯只读架构：Server 到 Agent 方向不存在任何控制通道，Agent 只采集和上报
2. 公网强制 TLS：所有公网通信强制加密；仅允许回环地址为受信隧道（Cloudflare Tunnel）提供明文；Agent 始终连接 wss:// 公网域名。证书 Pinning 仅用于直连自签场景，指纹由一键命令带外传入
3. 非 root 运行：Agent 始终以非特权用户运行
4. 无远程执行：代码中不引入 os/exec、pty、shell 等任何命令执行功能
5. 单管理员 + 强认证：不引入多租户
6. SSRF 防护：所有 Server 发起的对外请求（Webhook/Telegram/SMTP）做内网地址过滤
7. 最小权限采集：只采集不需要 root 的数据（GPU 走 NVML 库直读，禁止 exec）
8. 配置文件权限 600
9. 受限探测通道（S9）：HTTP Ping 仅 GET/HEAD、固定 UA、无自定义 header/body、响应体不读取、重定向 ≤2、目标数上限（全部 ≤64 / HTTP ≤32）、目标间并发 semaphore 默认 8
10. 凭据哈希：Agent Token、注册码、管理会话令牌只存 SHA-256；管理端使用不透明会话，禁止 JWT/jwt_secret
11. 主题：仅内置 CSS 主题，禁止第三方主题上传/市场/运行时 JS；公开页不接受外链背景图
12. Cloudflare：支持 Tunnel（推荐）和橙云 Origin Certificate；不支持 Workers 运行时

技术栈：Go 1.26.9（`database/sql` + `modernc.org/sqlite`，内嵌顺序迁移，禁用 GORM AutoMigrate；标准库 `net/http` + `github.com/coder/websocket`）+ React 19 + TypeScript + Vite + 自定义 CSS/SVG（无 shadcn/Tailwind/ECharts）+ pro-bing

通信方式：
- 注册/配置拉取：HTTPS REST（POST /api/v1/agent/register、GET /api/v1/agent/config）
- 证书指纹：仅直连自签时由管理员一键命令带外传入，不从同一 URL 现取
- 实时上报：WebSocket（强制 wss），Token 通过 Authorization: Bearer 头携带（禁止放 URL 查询参数，避免反代日志泄露）

WebSocket 消息协议（严格按 5.2 节，仅 4 种帧）：
- Agent → Server: report, ping_result, heartbeat
- Server → Agent: heartbeat_ack（仅此 1 种）
- 不存在 register/config_update 等任何下发帧；注册、配置变更全部走 REST 拉取

详细设计文档见：server_probe_design.md
```

### 10.2 M1 提示词：基础架构 + 核心采集

```
请实现 M1 里程碑：基础架构 + 核心采集。

按照 TDD 方式开发，先写测试再实现。

任务清单：
1. 初始化 Go module，创建目录结构（server/ 和 agent/ 两个子项目，共享 model 包）
2. 实现 Agent 采集器（internal/collector/）：
   - cpu.go: 读 /proc/stat 计算 CPU 使用率，读 /proc/cpuinfo 获取型号和核心数，读 /proc/loadavg 获取负载
   - memory.go: 读 /proc/meminfo 解析 MemTotal/MemAvailable/SwapTotal/SwapFree
   - disk.go: 系统调用 statfs 遍历挂载点获取磁盘使用率
   - network.go: 读 /proc/net/dev 计算网卡速率差值，读 /proc/net/tcp 和 /proc/net/udp 统计连接数
   - system.go: uname 系统调用获取系统信息，读 /proc/uptime 获取运行时间，遍历 /proc 统计进程数
3. 实现 Server 数据层（internal/repository/）：
   - sqlite.go: SQLite 连接管理（WAL 模式 + busy_timeout，原生 SQL + 内嵌顺序迁移；不使用 GORM）
   - ringbuffer.go: 内存环形缓冲（3 秒/点 × 3 小时 = 3600 点），支持并发安全写入读取，每 Agent 一个实例
4. 实现 Server 数据模型（internal/model/）：
   - agent.go, metric.go, alert.go, alert_history.go, notify.go, session.go
5. Agent 流量计数状态持久化：
   - state.go: 读写 /var/lib/probe-agent/state.json（上次累计字节数与时间戳），Agent 重启后据此续算速率，避免流量统计跳变；写盘节流 60 秒（防 SSD 磨损），跨月按 Agent 本地时区归零

验收标准：
- Agent 采集器能采集所有指标并输出 JSON 到 stdout
- Agent 重启后流量计数从 state.json 恢复，速率无跳变
- Server 能初始化 SQLite（WAL 生效）并通过 repository 层 CRUD
- 环形缓冲单元测试覆盖率 ≥ 90%
- 所有采集器不需要 root 权限

请参考设计文档第 4.1 节（采集项清单）、第 4.3 节（state_file 配置）和第 5.6 节（SQLite 表结构）。
```

### 10.3 M2 提示词：通信 + 注册上线

```
请实现 M2 里程碑：Agent-Server 通信 + 注册上线。

按照 TDD 方式开发。

任务清单：
1. Server 端 WebSocket 端点（/api/v1/agent/report）：
   - 连接管理（Agent 连接池 map[agentID]*Conn）
   - 消息分发（仅 report / ping_result / heartbeat 三种入站帧，回复 heartbeat_ack）
   - 连接建立时校验 Authorization: Bearer Token 与主机指纹
   - 心跳超时检测（90 秒无心跳标记离线）
2. Agent 端 WebSocket 客户端：
   - 连接维持，自动重连（指数退避 1s→60s）
   - 心跳发送（每 30 秒），Server 回 heartbeat_ack
   - 数据上报（每 3 秒打包采集数据发送）
3. REST 注册流程（不走 WebSocket，严格按 4.2 节 5 步）：
   - Server: POST /api/v1/agent/register，注册码 128-bit 随机（Base32 展示）、只存哈希、一次性、15 分钟过期（401/409 区分错误）；端点限速 5 次/分钟/IP
   - Server: 首次启动（admin 表为空）签发一次性 setup token（30 分钟有效），打印到控制台并写入 data 目录 setup_token 文件（权限 600）；setup 接口提交后作废，admin 非空后 /setup 返回 404
   - Server: Agent Token 随机 32 字节，数据库只存 SHA-256；创建/重置响应只返回一次明文；绑定主机指纹（指纹 = SHA256(install_salt + CPU型号 + 主网卡MAC + GOOS)，不含主机名，按 7.5 节）
   - Server: 管理会话使用 256-bit 不透明令牌，数据库只存 SHA-256；禁止 JWT/jwt_secret
   - Server: 支持 tls.mode 与受信代理；Cloudflare Tunnel 模式只信任回环来的 CF-Connecting-IP/CF-Visitor；橙云模式只信任配置的 Cloudflare CIDR
   - 发布流程: Agent 二进制 Ed25519 签名，安装脚本验签后再执行
   - Server: 指纹冲突返回 409，提示管理员调用 reset-fingerprint 接口
   - Agent: 首次启动用注册码注册获取 Token，保存到配置文件（权限 600）
4. TLS 与证书 Pinning：
   - Server: 支持 tls.mode=files/off；off 只允许绑定回环地址；公网路径始终有 TLS（本机或 Cloudflare 边缘终结）
   - 直连自签：指纹由管理员一键命令带外传入，写入 server_cert_fingerprints；Agent TLS 校验 = 系统证书链 OR 指纹匹配
   - Cloudflare/公网 CA：不启用叶子证书 Pinning，使用系统 CA；不调用 /api/v1/server-cert 作为信任来源
   - Agent 拒绝 http:// 和 ws:// 公网地址；tls_insecure 仅供本机调试且界面显示黄色徽章
5. 配置拉取：
   - Agent: 每小时 HTTPS GET /api/v1/agent/config 拉取探测目标配置（Authorization 头携带 Token）
   - 拉取失败使用本地缓存
6. 数据合理性校验：
   - Server: 校验上报数据范围（CPU 0-100%、内存 used≤total 等）
   - Server: 上报频率限制（过快拒绝，过慢标记离线）
   - Server: 数组长度限制（disk ≤32、ping_result ≤64）、hostname ≤128 字符、ipv4/ipv6 需 net.ParseIP 校验
   - report 帧携带 agent_version，Server 更新到 agents.agent_version（面板升级提示依赖）

WebSocket 消息协议严格按设计文档第 5.2 节实现（Server→Agent 仅 heartbeat_ack）。
安全要求严格按设计文档第 2 节和第 7 节实现。

验收标准：
- Agent 能通过 REST 注册接口用注册码换取 Token
- Agent 能维持 WebSocket 长连接，3 秒上报一次
- 注册码一次性消费，15 分钟过期；指纹冲突返回 409
- 公网全程 TLS；Tunnel 模式回环明文；直连自签 Pinning 生效；Cloudflare 模式使用系统 CA
- Agent Token、注册码、管理会话令牌在数据库中均无明文
- 断线自动重连（指数退避：1s→2s→4s→...→60s 上限）
```

### 10.4 M3 提示词：实时监控 + 前端面板

```
请实现 M3 里程碑：实时监控 + 前端面板。

任务清单（延迟真实数据在 M4 接入，本阶段用 mock）：
1. 前端项目初始化：
   - React 18 + TypeScript + Vite + shadcn/ui + Tailwind CSS + ECharts + Zustand + React Router
2. 设计系统基础组件（TDD：Vitest + Testing Library，mock 数据驱动测试）：
   - 玻璃拟态卡片（半透明背景 + backdrop-blur + 细边框）
   - RingProgress 圆环指标组件（CPU/内存/磁盘三圆环）
   - 延迟色阶常量（优秀绿/良好黄/较差橙/极差红，按 6.4.2 阈值）
   - LatencyGrid 延迟格子组件（色块网格，悬停显示该次完整统计；M3 用 mock 数据）
3. 登录页：
   - 密码登录，限速 5 次/分钟，连续失败 10 次锁定 15 分钟
   - 管理会话存于 HttpOnly Cookie（SameSite=Strict, Secure=true），2h 空闲/7d 绝对过期，服务端只存 token_hash
   - 可选 TOTP 两步验证
4. WebSocket 实时推送：
   - Server 端 /ws/dashboard 端点，管理会话认证 + Origin 校验
   - 每 3 秒推送所有在线 Agent 的实时数据
   - 前端 Zustand store 管理实时数据状态，断线重连 + 页面可见性恢复
5. 仪表盘卡片视图（NodeGet 风格玻璃拟态卡片）：
   - 国旗 + 标签彩色徽章行
   - CPU/内存/磁盘三 RingProgress 圆环指标
   - 实时上下行速度、月流量进度条（已用/配额）
   - 到期日期/月费用展示
   - 离线卡片整体降透明度处理
   - 点击卡片跳转详情页
6. 仪表盘表格视图：
   - 紧凑表格 + 迷你进度条 + 概览条
   - 延迟列动态生成（按 ping_targets 实际目标，含 v6 徽章，无 v6 目标显示"无v6"，M3 占位）
7. 筛选栏：
   - 搜索/标签/地区/IPv4/IPv6/排序组合筛选
   - 筛选状态 URL 参数持久化（刷新/分享后保持）
8. GeoIP 解析（TDD）：
   - 离线 mmdb 查询（按 5.7 节），Agent 公网 IP → 坐标/城市/国家码，写入 agents.geo_lat/geo_lon，随 API 下发
   - 无 mmdb 时优雅降级，管理员可手动设置 region/country_code 覆盖
9. 仪表盘地图视图：
   - ECharts 世界地图 + effectScatter 涟漪光点
   - 按坐标定位，同地区多机聚合，点击聚合点弹出服务器列表
10. 服务器详情页：
   - LatencyGrid 延迟格子图置顶（mock 数据）
   - CPU/内存实时折线图（ECharts）
   - 磁盘/网络/月流量卡片 + 信息与费用卡片
   - 时间范围切换：1H/6H/12H/1D/2D
   - 1H/6H 用环形缓冲实时数据（3 秒/点），WebSocket 推送刷新
   - 12H/1D/2D 用 SQLite 聚合数据（5 分钟/点），定时轮询
   - 6H 需混合数据源拼接（最近 3 小时实时 + 3-6 小时聚合，按 5.3 节聚合规则）
11. 元数据编辑 + 标签管理（TDD，设置页）：
   - 服务器显示名称/位置/到期日期/月费用/流量配额编辑
   - 标签 CRUD（彩色徽章），支持删除时二次确认
12. 主题系统：
   - 浅色/深色/跟随系统，localStorage 存储，CSS 变量切换
   - 背景切换：纯色/内置渐变/本地背景图（无外链 URL）
13. Go embed 前端：
   - web/ 为唯一 embed 源，前端构建产物通过 Go embed 内嵌到 Server 二进制
14. 首次初始化页（/setup）：
   - 输入一次性 setup token + 设置管理员密码（可选 TOTP），提交后 token 作废；admin 表非空时页面返回 404

验收标准：
- 管理员能登录面板（含锁定与静默续期）
- 仪表盘卡片/表格/地图三视图切换正常
- 卡片含圆环指标、月流量进度条、到期/费用展示，离线卡片降透明度
- 筛选组合生效且 URL 参数持久化
- 地图按 GeoIP 坐标定位光点，同地区多机聚合，点击可展开列表
- 详情页 CPU/内存折线图实时刷新，LatencyGrid 组件单测通过（真实数据 M4 接入）
- 服务器元数据与标签可在设置页编辑管理
- 主题与背景切换正常，且全部为内置 CSS 主题（运行时无第三方 JS/外链图片）
- 5 档时间范围切换正常，1H/6H 实时刷新
- 单二进制部署（前端内嵌）
```

### 10.5 M4 提示词：网络探测 + 历史数据

```
请实现 M4 里程碑：网络探测(Ping) + 历史数据。

按照 TDD 方式开发。

任务清单：
1. ICMP Ping 采集器（使用 pro-bing 库）：
   - privileged 模式（需 CAP_NET_RAW）
   - 10 个包采样，发包间隔 0.5 秒，总超时 15 秒
   - DNS 预解析排除 DNS 时间
   - 报告：avg_latency, min_latency, max_latency, jitter(StdDevRtt), loss, packets_sent, packets_recv
2. Unprivileged ICMP 降级：
   - Linux 3.0+ SOCK_DGRAM ICMP socket，无需 root
3. TCP Ping 采集器：
   - 5 次采样，每次超时 5 秒，间隔 0.5 秒
   - DNS 预解析
   - 报告 5 次平均延迟和失败比例
4. HTTP Ping 采集器：
   - 3 次采样，每次超时 10 秒
   - 自定义 DialContext 排除 DNS 时间
   - 状态码 2xx-3xx 为成功
5. Ping 方法自动选择：
   - 优先 privileged ICMP → 降级 unprivileged ICMP → 降级 TCP Ping
   - 安装时 setcap 检测，运行时探测方式标注
6. 探测目标配置同步：
   - Agent 每小时拉取服务端配置的探测目标列表
   - 目标字段含 ip_version（v4/v6），命名遵循 4.5.1 节规则（如 电信v4、阿里v6）
   - 首次部署预置 5 个 is_default=1 种子目标（电信v4/阿里v4/腾讯v4/阿里v6/Google v6）
   - 本地缓存，拉取失败用缓存
   - 默认每 60 秒执行一轮完整探测
7. 数据聚合落盘：
   - 每 5 分钟将环形缓冲实时数据聚合为一个点写入 SQLite
   - 聚合规则严格按 5.3 节：CPU/内存/负载取平均，磁盘按挂载点分别平均，网络取平均速率，Ping 取 avg/min/max/loss
8. 历史数据查询 API：
   - GET /api/v1/servers/:id/history?range=1h|6h|12h|1d|2d
   - 1h/6h 从环形缓冲读取，12h+ 从 SQLite 读取
9. 历史数据清理：
   - 定时清理超期数据（默认 90 天）
10. 延迟格子数据接入：
   - M3 的 LatencyGrid 组件接入真实 Ping 历史（环形缓冲最近 60 分钟）
   - 卡片/表格/详情页三处统一接入，悬停显示该次完整统计
11. 延迟丢包融合图：
   - 详情页三网延迟折线（每个 ip_version 目标一条线）+ 丢包率柱状图共享 X 轴
12. 探测并发调度：
   - 目标间并发执行，全局 semaphore 默认 8（Server 配置可调）
   - 目标数上限：单 Agent 全部目标 ≤64（HTTP ≤32），Server 下发前校验，Agent 端兜底截断

Ping 方案必须超越 Nezha 的准确性，参考设计文档第 4.6 节。

验收标准：
- ICMP Ping 10 包采样，报告完整统计（平均/最小/最大/抖动/丢包率）
- Ping 方法自动降级正常
- 历史数据每 5 分钟按 5.3 节规则落盘
- 历史数据查询 API 正常
- 仪表盘卡片/表格/详情页延迟格子展示真实探测数据，悬停显示完整统计
- 融合图按目标 ip_version 区分 v4/v6 线条
- 超期数据自动清理
```

### 10.6 M5 提示词：告警 + 通知 + 安全加固

```
请实现 M5 里程碑：告警 + 通知 + 安全加固。

按照 TDD 方式开发。

任务清单：
1. 告警规则 CRUD（API + 前端管理页）：
   - 规则字段：name, metric(cpu_usage/mem_usage/disk_usage/agent_offline/traffic_quota/expire_days), operator(>/</=), threshold, duration, enabled, scope(JSON: all/agent_ids/tag_ids), notify_channel_ids(JSON数组)
   - metric 语义严格按 5.4 节：traffic_quota 传月流量已用百分比（operator 通常 >），expire_days 传剩余天数（operator 通常 <）
2. 告警状态机（状态持久化）：
   - 状态：OK → PENDING(超阈值但未达duration) → FIRING(达到duration) → RESOLVED(恢复正常)
   - 仅对 scope 匹配的 Agent 评估（all / 指定 agent_ids / 指定 tag_ids 的成员机）
   - 状态写入 alert_history 表（rule_id/agent_id/status/value/started_at/updated_at/notified），Server 重启后恢复现场，不重复通知
   - 进入 FIRING 时发送告警通知，进入 RESOLVED 时发送恢复通知
3. 通知去重 + 静默期：
   - 同一告警 FIRING 状态下，静默期内（默认 60 分钟）不重复发送
   - 通知内容：固定模板 + 变量替换（{hostname}/{rule_name}/{metric}/{value}/{time}），告警与恢复各一套中英文默认文案
4. Webhook 通知 + SSRF 防护（重点安全）：
   - 内网地址过滤：10/8, 172.16/12, 192.168/16, 127/8, 169.254/16, ::1, fc00::/7
   - DNS 重绑定防护：自定义 Dialer 强制使用预解析 IP
   - 重定向检测：CheckRedirect 中再次 SSRF 检查
   - 响应体限制：最多读 1KB，不反射给用户
   - 超时 10 秒，强制 TLS 验证
5. Telegram 通知：Bot API 发送（同套 SSRF 防护）
6. 邮件通知：SMTP 发送，SMTP 主机名走独立 Dialer SSRF 检查（按 7.4 节）
7. TOTP 两步验证：TOTP 密钥生成/验证，启用时生成 10 个一次性恢复码（只存 SHA256，用后即删）
8. 公开分享页：与仪表盘同款三视图 UI（卡片/表格/地图），share_id 为 UUIDv4，白名单字段仅展示非敏感信息（无 IP/Token/配置），站点标题/Logo/页脚可配置；实时数据经 /ws/public/:share_id 推送（share_id 鉴权 + 白名单字段 + 同 share_id 最多 5 连接）
9. 安全响应头中间件：CSP / HSTS / X-Frame-Options / Referrer-Policy / nosniff
10. 安全审计：按设计文档第 7.7 节清单逐项审计，含 7.2 节可执行文件审计规则（Agent 二进制零匹配 os/exec 等）

SSRF 防护实现参考设计文档第 7.4 节。

验收标准：
- 告警规则能正确触发和恢复，含 traffic_quota/expire_days 两类指标，作用域与多渠道生效
- Server 重启后告警状态从 alert_history 恢复，不重复发送
- Webhook/Telegram/SMTP 通知均经过 SSRF 防护，内网地址被拒绝
- 通知去重和静默期生效，通知内容含模板变量替换
- TOTP 两步验证可用，恢复码可用
- 公开分享页三视图正常且无敏感字段，公开页 WS 实时推送正常
- 安全响应头生效
- 安全审计清单全部通过
```

### 10.7 M6 提示词：部署 + 发布 + 文档

```
请实现 M6 里程碑：部署 + 发布 + 文档。

任务清单：
1. Docker 镜像构建：
   - 多阶段构建（Node 构建前端 → Go 构建后端 → Alpine 运行时）
   - 多架构（linux/amd64, linux/arm64）
   - 安全加固：read_only, no-new-privileges, cap_drop ALL
2. Server 一键安装脚本：
   - 检测架构，下载二进制（SHA-256 + Ed25519 验签），创建配置，安装 systemd service
   - 首次启动不生成 JWT 密钥；管理端使用不透明会话（token_hash 存库）
   - Schema 变更走仓库自带 embed 顺序迁移（`server/internal/repository/migrate.go`），禁用 GORM AutoMigrate
3. Agent 一键安装脚本（严格按 8.3 节）：
   - 解析参数（--server, --code, 可选 --cert-fingerprint）
   - 检测系统和架构（仅支持 Linux）
   - 下载二进制 + .sig（SHA-256 + Ed25519 验签）
   - 创建 probe 用户
   - 生成 install_salt（openssl rand -hex 32）
   - 仅直连自签模式：把一键命令里的 --cert-fingerprint 带外写入 server_cert_fingerprints；不从同一 URL 现取
   - 创建配置文件（权限 600）与状态目录 /var/lib/probe-agent（chown -R probe:probe）
   - 尝试 setcap cap_net_raw+ep（仅非 systemd 场景备用；systemd 由 AmbientCapabilities 授予，见 4.8）
   - 尝试 sysctl -w net.ipv4.ping_group_range（unprivileged ICMP 前置，失败如实降级并标注）
   - 安装 systemd service（User=probe, ReadWritePaths 含状态目录, 安全加固）
   - 启动服务
4. Agent 升级脚本（手动升级）：
   - 停止服务，备份旧版，替换新版，重新 setcap，启动服务
   - 升级不动配置与 state.json：install_salt 不变则主机指纹不变，避免误触发 409
5. GitHub Release CI/CD：
   - 自动构建 Linux 平台二进制（amd64/arm64）+ SHA256 + Ed25519 签名
   - 自动构建多架构 Docker 镜像
6. 用户文档：安装/配置/使用/FAQ（含证书指纹轮换、指纹重置、流量基线重置说明）
7. 安全文档：SECURITY.md，安全报告流程

注意：第一版不做 Agent 自动升级（违反纯只读架构）；v1 仅发布 Linux 二进制。

验收标准：
- Docker 部署成功
- 一键安装脚本在 Ubuntu/Debian/CentOS 上测试通过
- GitHub Release 包含 linux/amd64 与 linux/arm64 二进制、SHA256 和 Ed25519 签名（.sig）
- 文档覆盖安装、配置、使用
```

---

## 附录：Nezha 漏洞参考

以下漏洞信息用于理解本探针安全设计的背景和必要性。

| 漏洞 | 严重程度 | 描述 | 我们的防御 |
|------|---------|------|-----------|
| CVE-2026-46716 | Critical (9.9) | 跨租户 Cron RCE，RoleMember 可在所有服务器执行任意命令 | S1+S4：无控制通道，无命令执行 |
| GHSA-q6xx-5vr8-p898 | Critical (9.9) | 终端/文件管理器会话劫持，跨租户 RCE | S1+S4：无终端/文件管理器 |
| CVE-2026-46717 | High (7.7) | 通知 Webhook SSRF + 完整响应体反射 | S6：SSRF 防护层 |
| CVE-2026-47120 | High (7.1) | 告警规则触发他人 Cron 任务 | S1+S4：无 Cron 任务 |
| CVE-2026-48119 | High (7.1) | Agent 伪造他人服务监控结果 | S5：单管理员无跨租户问题 + 数据校验 |
| CVE-2026-49396 | Moderate | CSRF 触发存储的 Cron 命令 | S5：SameSite=Strict + 无 Cron |
| CVE-2026-47124 | Moderate (6.5) | WebSocket 跨租户遥测泄露 | S5：单管理员 + WS 认证 |
| NEZHA-AGENT-001 | High (7.5) | Agent 明文 gRPC 通道，凭证可被截获 | S2：公网强制 TLS（仅受信隧道回环段例外） |

---

**文档结束**

本文档包含了服务器探针的完整设计规格和开发提示词。使用时：
1. 先通读全文理解整体设计
2. 按 M1→M6 顺序使用 AI 开发提示词
3. 每个里程碑完成后对照验收标准检查
4. 开发完成后按第 7.7 节安全审计清单审计
