# XProbe 服务器探针开发计划与设计文档

> **文档用途**：本文档既是产品设计规格，也是可直接用于 AI 编程工具（如 TRAE、Cursor、Claude）的开发提示词。文档包含完整的技术规格、架构设计、安全要求、开发里程碑，以及每个设计决策的原因解释。
>
> **生成日期**：2026-06-21
> **更新日期**：2026-08-31
> **版本**：v1.3
>
> **v1.3 变更**（基于 v1.2 全文修订，项目定名 XProbe）：
>
> 1. **范围调整**：地图视图 + GeoIP（原 5.7）、TOTP 两步验证移入 v2（新增第 11 章 v2 路线图）；v1 保留公开分享页与月流量配额/到期成本管理；仪表盘为卡片/表格双视图（视图切换组件预留地图入口位）；新增 M0 设计系统里程碑，总工期重估约 11~12 周。
> 2. **一致性修复**：Agent 上报帧示例去除 `token` 字段（与 5.2 协议统一——Token 仅在 WS 握手 Authorization 头携带，帧内不携带）；注册码传输表述统一为 HTTPS REST；5.1 API 表补齐删除服务器、指纹重置、Agent Token 重置、会话管理接口；JWT 有效期统一为 2 小时 + 静默续期；5.6 补 sessions 表 DDL；8.3 安装脚本补齐 install_salt 生成、证书指纹写入、状态目录属主等步骤。
> 3. **安全增强**：Agent Token 与注册码在服务端仅存 SHA256 哈希（新增安全原则 S9）；注册接口 IP 限速 + 全局 API 限速中间件；新增安全响应头与 CORS 策略（7.7）；分享页 logo_url 仅允许 https scheme；WS 读限制 64KB；会话管理（列出/吊销/登出全部设备）；新增 `probe-server reset-password` CLI（管理员密码找回）；安全审计 CI 化（gosec + gitleaks + Agent 二进制 `os/exec` 零匹配检查）。
> 4. **工程可行性**：GPU 采集改为纯 Go 运行时 dlopen NVML（去 cgo，保障单命令交叉编译）；安装脚本写入 `net.ipv4.ping_group_range`（否则 unprivileged ICMP 降级链失效）；WS 重连指数退避加 ±20% jitter + WS close 事件即时离线检测；月流量自然月边界统一 UTC；CPU 首采样置空（避免启动假值）；WS 上报可选 permessage-deflate 压缩；备份改为 `sqlite3 .backup`（WAL 模式下直接 cp 不一致）；新增 `metric_records_daily` 日聚合表与详情页 7d/30d 视图；删除 Agent 时级联清理数据；Agent 二进制改由 Server 内嵌分发（`/download` 自包含，不依赖 GitHub）。
> 5. **前端**：新增 M0 设计系统流程（ui-ux-pro-max → frontend-design → frontend-skill → shadcn 语义 token）；延迟色阶改为双主题 CSS 变量（浅色主题对比度 ≥ 4.5:1）；全局尊重 `prefers-reduced-motion`；空状态/加载态规范；i18n 字符串集中管理（v1 仅中文，预留英文）。
>
> v1.2 变更：全文档一致性审查修订——第 10 章提示词与设计主体同步；注册统一为 REST 通道，删除 Server→Agent 推送帧；新增 Agent 证书 Pinning 防 MITM；Agent Token 改 Header 传输；JWT 缩短有效期并支持吊销；SSRF 防护扩展至 SMTP；补齐 alert_history/sessions 表与删除/指纹重置等缺失 API；修正环形缓冲计算与聚合规则；修复安装脚本目录属主；GPU 采集改 NVML 库绑定；明确 v1 仅支持 Linux。
>
> v1.1 变更：前端 UI 全面改版对标 NodeGet 风格；新增标签分组、多视图仪表盘、延迟格子图、IPv4/IPv6 双栈、流量统计、VPS 到期/成本管理等功能。

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
11. [v2 路线图](#11-v2-路线图)
12. [附录：Nezha 漏洞参考](#12-附录nezha-漏洞参考)

---

## 1. 项目概述

### 1.1 项目背景

本项目旨在开发 **XProbe**——一款**以安全为核心**的服务器探针（监控）系统。起因是 2026 年 5 月主流探针 Nezha（哪吒探针）集中爆发了 9 个安全漏洞（含 2 个严重级别的跨租户 RCE），导致大量部署 Nezha 的服务器被入侵。

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

NodeGet（NodeSeekDev 出品的 Rust 探针）及其社区主题是当前 VPS 圈公认的高颜值代表。XProbe 前端对标其视觉风格，**并按 M0 设计系统流程做视觉升级**——布局与信息架构保留，配色/字体/质感/无障碍打磨到高于社区主题的完成度：

| NodeGet UI 特征 | XProbe 采纳方式 |
|-----------------|---------------|
| CPU/内存/磁盘**圆环**进度展示 | 卡片内三个小圆环，替代传统进度条 |
| **延迟小格子**图（延迟越高格子越高、颜色柔和渐变） | 卡片与详情页核心组件 |
| **卡片/表格/地图三视图** | **v1 采纳卡片/表格双视图**；地图视图依赖 GeoIP，随 GeoIP 一并移入 v2，视图切换组件预留第三入口位 |
| 标签筛选、地区筛选、搜索排序 | 仪表盘顶部筛选栏 |
| IPv4 / IPv6 Ping 分开展示 | 双栈延迟独立展示 |
| 玻璃拟态（Glassmorphism）卡片 + 可切换背景 | 毛玻璃卡片 + 背景（纯色/渐变/自定义），细节按 M0 设计系统升级 |
| 浅色/深色模式 + 柔和配色 | 主题系统支持（延迟色阶做双主题对比度校准） |
| VPS 到期时间 / 剩余价值展示 | 服务器元数据（到期/成本/币种）+ 到期提醒告警 |
| 移动端适配 | 响应式布局 |

**注意**：NodeGet 的部分功能**不采纳**（与安全红线冲突）或**延后**（范围控制）：
- Js Worker 插件系统（服务端执行任意 JS，违反 S4 无远程执行）——**不采纳**
- 完全前后端分离的静态部署（增加部署复杂度，与单二进制定位冲突）——**不采纳**
- 细粒度多用户权限（违反 S5 单管理员原则）——**不采纳**
- Server 主动 Ping/IP 查询功能（我们是 Agent 本地探测后上报，不由 Server 发起）——**不采纳**
- 地图视图 + GeoIP 离线库、TOTP 两步验证——**移入 v2**（非安全冲突，属范围控制，见第 11 章）

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
| **历史数据粒度** | 5 分钟聚合 + 日聚合双档（v1.3 新增） | 5 分钟粒度保 90 天明细，日聚合保 365 天趋势，支撑详情页 7d/30d 视图 |
| **月流量月界** | 统一 UTC（v1.3 新增） | 消除 Agent/Server 时区不一致导致的月度统计错位 |
| **凭证存储** | Agent Token/注册码仅存 SHA256 哈希（S9，v1.3 新增） | 数据库文件泄露不等于凭证泄露 |
| **Agent 二进制分发** | Server 内嵌 Agent 二进制（v1.3 定案） | 一键安装完全自包含（不依赖 GitHub 外网），代价为 Server 体积 +40~60MB，符合单二进制定位 |
| 告警通知 | 基础告警 + SSRF 防护 + 去重（C） | 对症防范 Nezha SSRF 漏洞，避免告警风暴 |
| **v1 功能范围** | 分享页 + 流量/成本管理保留；地图+GeoIP、TOTP 移 v2（v1.3 定案） | 控制首版工期，核心监控链路优先 |
| 前端面板 | 标准面板 + 主题/自定义/分享（C） | 覆盖核心场景 + 个性化体验 |
| 前端 UI 风格 | 对标 NodeGet（圆环卡片/延迟格子/双视图/玻璃拟态）+ M0 视觉升级 | VPS 圈公认高颜值风格，社区验证过的审美，视觉完成度更高 |
| 部署支持 | 全平台多架构（D） | Go 交叉编译成本低，覆盖 VPS/NAS/树莓派/PC |
| 架构方案 | 经典分层架构（方案一） | 清晰分层适合 AI 工具按层开发，扩展性恰到好处 |

---

## 2. 核心安全原则

这一节定义了整个系统的安全基线，所有后续设计都必须符合这些原则。这些原则是不可妥协的设计红线。

### 2.1 安全原则清单

| 编号 | 原则 | 说明 | 对标 Nezha 漏洞 |
|------|------|------|----------------|
| S1 | **纯只读架构** | Server 到 Agent 方向不存在任何控制通道。Agent 只采集和上报，Server 不下发任何指令。 | 根除 CVE-2026-46716（Cron RCE）、GHSA-q6xx-5vr8-p898（终端劫持）的攻击面 |
| S2 | **强制 TLS** | 所有通信（Agent↔Server、浏览器↔Server）强制加密，不允许关闭。Agent 拒绝连接未启用 TLS 的 Server，并通过证书指纹 Pinning 防 MITM。 | 根除 NEZHA-AGENT-001（明文 gRPC 凭证泄露） |
| S3 | **非 root 运行** | Agent 始终以非特权用户运行，通过 setcap 获取最小能力（仅 ICMP），不支持则降级 TCP Ping。 | 降低 RCE 发生时的危害 |
| S4 | **无远程执行** | 代码中不存在任何"执行命令""建立终端会话""操作文件"的功能。这是 S1 的代码级保障，并由 CI 自动验证（见 7.2）。 | 根除所有 RCE 类漏洞的根源 |
| S5 | **单管理员 + 强认证** | 第一版单管理员，强密码 + 登录限速 + 会话管理。不引入多租户，避免 BOLA/IDOR 类漏洞。 | 根除 CVE-2026-46716 等 9 个漏洞的根因（多租户授权缺失） |
| S6 | **SSRF 防护** | 所有对外发起 HTTP 请求的功能（Webhook 通知、SMTP 发信）做内网地址过滤、禁止重定向到内网、限制响应体大小。 | 针对 GHSA-w4g9-mxgg-j532（SSRF + 响应体反射） |
| S7 | **最小权限采集** | Agent 只采集不需要 root 的数据。需要 root 的数据（如 SMART、其他用户进程详情）不采集或标注"不可用"。 | 减少攻击面 |
| S8 | **配置文件权限控制** | Agent 配置文件（含 Token）权限 600，仅属主可读写。 | 防止本地凭证泄露 |
| S9 | **凭证哈希存储**（v1.3 新增） | Agent Token、注册码在服务端数据库中仅存 SHA256 哈希，不存原文。数据库文件泄露不等于凭证泄露。 | 纵深防御：应对数据库文件/备份/快照意外泄露场景 |

### 2.2 数据流方向（关键安全特性）

```
Agent ────采集数据 + 探测结果──────────▶ Server  (主动推送, WS)
Agent ◀───探测目标配置(只读JSON)────────  Server  (Agent 主动拉取, HTTPS GET)
浏览器 ────查看请求─────────────────────▶ Server  (REST API)
浏览器 ◀───实时数据推送────────────────  Server  (WebSocket)
```

**关键点**：Server 永远不会主动向 Agent 发起连接。Agent 主动连接 Server 并维持 WebSocket，数据流是 Agent→Server 单向。配置同步也是 Agent 主动发起 HTTPS GET 拉取，不是 Server 推送。这意味着即使 Server 被攻破，攻击者也无法通过 Server 向 Agent 下发任何东西——因为 WebSocket 连接是 Agent 发起的，且协议中只定义了 Agent→Server 的数据帧和 Server→Agent 的心跳确认帧，没有"命令"帧。

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
│  │  (Gin)      │  │  (service/)  │  │  (repo/)     │  │ (embed)  │  │
│  │  - REST     │  │  - monitor   │  │  - SQLite    │  │          │  │
│  │  - WebSocket│  │  - alert     │  │  - RingBuffer│  │          │  │
│  │  - Auth     │  │  - notify    │  │              │  │          │  │
│  │  - 限速/安全头│  │  - config   │  │              │  │          │  │
│  └──────┬──────┘  └──────────────┘  └──────────────┘  └──────────┘  │
│  ┌──────────────────────────────────────────┐  ┌──────────────────┐  │
│  │ /download/agent/:os/:arch                │  │ 内嵌 Agent 二进制 │  │
│  │ (Agent 分发端点, 供一键安装脚本下载)        │  │ (go:embed)       │  │
│  └──────────────────────────────────────────┘  └──────────────────┘  │
└─────────┬────────────────────────────────────────────────────────────┘
          │ WebSocket (wss, 强制TLS, 可选压缩)
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

**v1.3 变更**：Server 二进制内嵌全部架构的 Agent 二进制（`go:embed`），一键安装脚本从 Server 自身的 `/download/agent/:os/:arch` 端点下载——安装流程完全自包含，不依赖 GitHub 等外部网络。

### 3.2 技术栈

| 层级 | 技术 | 理由 |
|------|------|------|
| 后端语言 | Go 1.22+ | 单二进制、跨平台、并发强、Nezha/Komari 验证过 |
| Web 框架 | Gin | 成熟、性能好、中间件生态丰富 |
| 通信协议 | WebSocket + JSON（强制 wss，gorilla/websocket） | 强制 TLS、复用 HTTPS 证书、JSON 易调试；可选启用 permessage-deflate 压缩 |
| 数据层 | 原生 SQL（database/sql + mattn/go-sqlite3）为主，GORM 仅做行映射 | 建表与索引可精确控制，避免 AutoMigrate 的隐式行为 |
| 数据库 | SQLite（内嵌，WAL 模式） | 零配置、单文件、适合个人十几台规模 |
| 实时数据 | 内存环形缓冲 | 吸收高频写入，前端读内存快 |
| 前端框架 | React 18 + TypeScript | 生态成熟、组件化适合主题/布局/分享 |
| 前端构建 | Vite | 快速构建，产物供 Go embed |
| UI 库 | shadcn/ui | 基于 Radix UI + Tailwind，语义 token 可定制性强 |
| 图表库 | ECharts | 性能好、支持实时数据流、图表类型丰富（注：shadcn Charts 组件包装的是 Recharts，与本方案冲突，图表一律 ECharts 自建封装，shadcn 仅负责外壳） |
| 状态管理 | Zustand | 轻量、适合中等复杂度 |
| ICMP 库 | pro-bing（prometheus-community） | Nezha 同款、活跃维护、统计信息丰富 |
| GPU 采集 | 纯 Go 运行时 dlopen NVML（如 purego/自封装 dlopen） | **v1.3 变更**：go-nvml 需要 cgo，会破坏"单命令交叉编译 amd64/arm64"；改为运行时动态加载 libnvidia-ml.so，无 cgo、零 exec，探测不到 NVML 库时优雅显示"不可用" |

### 3.3 Server 目录结构

```
server/
├── cmd/
│   └── server/
│       ├── main.go              # 入口, 初始化配置/数据库/路由, 启动服务
│       └── cli.go               # CLI 子命令: reset-password (管理员密码找回, v1.3 新增)
├── internal/
│   ├── api/                     # API层 (HTTP路由处理)
│   │   ├── router.go            # 路由注册
│   │   ├── middleware.go        # 中间件(认证/限速/请求日志/安全响应头, v1.3 补安全头)
│   │   ├── handler_auth.go      # 登录/登出/会话管理(v1.3: 列出/吊销会话)
│   │   ├── handler_server.go    # 服务器列表/详情/删除(级联清理)/指纹重置
│   │   ├── handler_agent.go     # Agent WebSocket接入/REST注册/配置拉取/Token重置
│   │   ├── handler_alert.go     # 告警规则CRUD + 告警历史
│   │   ├── handler_notify.go    # 通知渠道CRUD
│   │   ├── handler_config.go    # 探测目标/系统设置
│   │   ├── handler_download.go  # /download/agent/:os/:arch 内嵌二进制分发(v1.3 新增)
│   │   └── handler_public.go    # 公开分享页
│   ├── service/                 # 业务层
│   │   ├── monitor.go           # 实时数据管理(Agent连接池+环形缓冲)
│   │   ├── alert.go             # 告警引擎(阈值检测+状态机)
│   │   ├── notify.go            # 通知发送(SSRF防护+去重+静默期)
│   │   ├── agent_registry.go    # Agent注册/Token管理(哈希存储)/注册码/指纹
│   │   ├── aggregator.go        # 5分钟聚合落盘 + 每日聚合落盘(v1.3 新增日聚合)
│   │   └── config_sync.go       # 探测目标配置同步
│   ├── repository/              # 数据层
│   │   ├── sqlite.go            # SQLite连接管理(WAL+busy_timeout)
│   │   ├── repo_agent.go        # Agent元数据CRUD(级联删除)
│   │   ├── repo_alert.go        # 告警规则/历史CRUD
│   │   ├── repo_notify.go       # 通知渠道CRUD
│   │   ├── repo_record.go       # 历史聚合数据CRUD(5分钟+日聚合)
│   │   ├── repo_session.go      # 会话CRUD(v1.3 新增)
│   │   └── ringbuffer.go        # 内存环形缓冲(实时数据)
│   ├── model/                   # 数据模型(共享)
│   │   ├── agent.go             # Agent/ServerInfo
│   │   ├── metric.go            # 监控指标结构(含日聚合)
│   │   ├── alert.go             # 告警规则模型
│   │   ├── notify.go            # 通知渠道模型
│   │   └── session.go           # 登录会话模型(v1.3 新增)
│   └── pkg/                     # 工具包
│       ├── auth.go              # JWT/密码哈希/会话管理
│       ├── ssrf.go              # SSRF防护(内网过滤/重定向检测)
│       ├── tls.go               # 强制TLS/证书管理
│       ├── securityheader.go    # 安全响应头中间件(v1.3 新增)
│       └── embed.go             # 前端静态资源 + Agent 二进制 embed
├── assets/
│   └── agents/                  # 构建时放入 linux-amd64/linux-arm64/linux-armv7 的 agent 二进制
│                                # (go:embed, 供 /download 端点分发; v1.3 新增)
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
│   │   ├── gpu.go               # GPU使用率(纯Go dlopen NVML, 如可用, 非root, v1.3 改)
│   │   └── ping.go              # 三网延迟/丢包(ICMP或TCP)
│   ├── reporter/                # 上报器
│   │   ├── ws.go                # WebSocket客户端, 维持长连接(重连退避+jitter)
│   │   ├── heartbeat.go         # 心跳维持(每30秒)
│   │   └── upload.go            # 数据打包上报(每3秒)
│   ├── config/                  # 配置拉取器
│   │   └── sync.go              # 定时HTTPS GET拉取探测目标配置
│   ├── state/                   # 状态持久化
│   │   └── state.go             # 流量累计等状态读写 state.json
│   └── register/                # 注册器
│       └── register.go          # 首次启动用注册码经 HTTPS REST 换取持久Token
├── go.mod
└── go.sum
```

---

## 4. Agent 详细设计

### 4.1 采集项清单与权限要求

| 采集项 | 数据来源 | 是否需要root | 备注 |
|--------|---------|-------------|------|
| CPU使用率 | `/proc/stat` | 否 | 两次采样差值计算；**Agent 启动后首个采样周期值置空（null），前端显示 `--`**（v1.3：避免单采样产生假 0%/100%） |
| CPU型号/核心数 | `/proc/cpuinfo` | 否 | 读取即可 |
| 负载(1/5/15分) | `/proc/loadavg` | 否 | 读取即可 |
| 内存/Swap | `/proc/meminfo` | 否 | 读取即可 |
| 磁盘使用率 | 系统调用(statfs) | 否 | 读取即可 |
| 磁盘IO | `/proc/diskstats` | 否 | 读取即可 |
| 网卡流量速率 | `/proc/net/dev` | 否 | 两次采样差值计算速率 |
| **月度流量累计** | `/proc/net/dev` | 否 | Agent 维护累计字节数并持久化到 `state_file`（重启后从持久化值续算），**月界统一按 UTC 判定**（v1.3：消除 Agent/Server 时区错位），用于流量配额展示 |
| TCP/UDP连接数 | `/proc/net/tcp`, `/proc/net/udp` | 否 | 读取即可 |
| 进程数 | `/proc` 遍历 | 否 | 只统计数量,不读其他用户进程详情 |
| 运行时间 | `/proc/uptime` | 否 | 读取即可 |
| 系统信息 | `uname` 系统调用 | 否 | 读取即可 |
| **IPv4/IPv6 地址** | 网卡地址/出站连接探测 | 否 | 注册时上报公网出口 IPv4/IPv6（通过连接 Server 的本地 socket 获取，无需访问外部服务），运行时定期更新 |
| GPU使用率 | 纯 Go dlopen `libnvidia-ml.so`（NVML） | 否 | 仅 NVIDIA；**v1.3 变更：不使用 cgo**——默认构建不含 GPU 代码路径，运行时探测到 NVML 动态库时加载启用，失败显示"不可用"。**禁止**调用 nvidia-smi 等外部命令（S4 要求 Agent 零 exec） |
| ICMP Ping | 原始套接字 | 需`CAP_NET_RAW` | setcap赋予,不支持则降级 |
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
│              │                │ 6. 从Server   │
│              │                │   下载Agent   │
│              │                │ 7. 创建probe用户│
│              │                │ 8. 生成install_salt│
│              │                │ 9. 获取证书指纹 │
│              │                │10. setcap/服务 │
│              │                │11. HTTPS REST │
│              │                │   注册换Token │
│ 12. 收到注册  │ ◀──WebSocket──│12. wss连接    │
│    服务器上线 │                │   持续上报    │
└──────────────┘                └──────────────┘
```

**一键命令示例**：

```bash
curl -fsSL https://your-server.com/install.sh | bash -s -- --server https://your-server.com --code ABC123XY
```

**注册码安全机制**（比 Komari 更安全）：

| 属性 | 设计 |
|------|------|
| 有效期 | 生成后 15 分钟内有效，超时自动失效 |
| 使用次数 | 一次性，注册成功后立即失效 |
| 绑定信息 | 注册成功后绑定 Agent 的主机指纹（install_salt + CPU 型号 + 主网卡 MAC + 系统类型），防止 Token 被盗用到其他机器 |
| 数量限制 | 每个管理员最多 5 个未使用的注册码同时存在 |
| 传输安全 | 注册码通过 **HTTPS REST**（`POST /api/v1/agent/register`）传输，全程 TLS 保护；**v1.3 修正**：v1.2 此处误写为"wss 传输"，注册通道自 v1.2 起已统一为 REST |
| **接口限速**（v1.3 新增） | 注册接口 IP 限速 5 次/分钟/IP，注册码爆破不可行 |
| **服务端存储**（v1.3 新增） | 注册码在服务端仅存 SHA256 哈希（S9），数据库泄露不泄露可用注册码 |

**注册流程**（v1.2 起统一走 REST，不走 WebSocket）：
1. Agent 首次启动，配置文件中只有 Server 地址和注册码，无 Token
2. Agent 通过 HTTPS `POST /api/v1/agent/register` 发送注册请求（注册码 + 主机基本信息 + 主机指纹）
3. Server 验证注册码有效性和有效期，生成持久 Token，返回给 Agent
4. Agent 将 Token 保存到配置文件（权限 600），注册码从配置中删除
5. 后续所有通信使用 Token 认证（WS 握手 Authorization 头 / REST Bearer 头）
6. Server 标记注册码已使用，记录 Agent 的主机指纹

### 4.3 Agent 配置文件

```yaml
# /etc/probe-agent/config.yml (权限: 600, 属主: probe)
server: "https://your-server.com"    # Server地址
token: "persistent-token-xxx"        # 注册后获得,首次安装时为空
# register_code: "ABC123XY"          # 首次安装时有,注册后删除
install_salt: "随机64位hex"           # 安装时生成,参与主机指纹计算(防跨机盗用)
server_cert_fingerprints:            # 允许的Server证书SHA256指纹列表(适配自签/私有CA)
  - "ab12cd34..."                    # 支持[旧,新]双指纹平滑轮换
tls_insecure: false                  # 仅本地调试,生产禁用;开启时面板标注"证书未验证"
state_file: "/var/lib/probe-agent/state.json"  # 流量累计等状态持久化(属主probe,权限600)
report_interval: 3                   # 上报间隔(秒)
config_sync_interval: 3600           # 配置拉取间隔(秒)
ping_method: "auto"                  # auto/icmp/tcp, auto=优先ICMP降级TCP
```

### 4.4 Agent 上报数据格式（JSON over WebSocket）

**v1.3 修正**：帧内**不携带 Token**——Token 在 WS 握手时经 `Authorization: Bearer` 头校验（v1.2 旧示例中的 `token` 字段为改版残留，已删除）。

```json
{
  "type": "report",
  "timestamp": 1718900000,
  "hostname": "web-server-01",
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
- Agent 重启后从本地状态文件中恢复上次累计值，续算而非清零（`/proc/net/dev` 计数器重启归零，需 Agent 自行持久化）
- **跨自然月时归零重新累计；月界按 UTC 判定**（v1.3：Agent 与 Server 口径一致）
- Server 端以「当月内收到的最大累计值」为准（避免乱序），按月落盘归档
- 该数据用于面板的月流量进度条和超量告警

### 4.5 Ping 探测数据格式

（同样不携带 Token，见 4.4 说明）

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

#### 比 Nezha 更准确的 5 个原因

| 原因 | XProbe | Nezha | Komari | 影响 |
|------|-------|--------|--------|------|
| ICMP 采样数 | 10 个包取平均 | 5 个包 | 1 个包 | 最关键，10 包平均能平滑抖动、容忍部分丢包 |
| 超时时间 | 15 秒 | 20 秒 | 3 秒 | 高延迟链路上 Komari 易误判失败 |
| 统计口径 | avg/min/max/jitter/loss 全量报告 | 仅 avg | avg | 抖动与极值反映网络稳定性 |
| 重试策略 | 无（原样报告） | 无 | 高延迟重试 | Komari 的重试系统性丢弃真实高延迟数据，引入偏差 |
| 丢包率统计 | 10 包中部分丢包仍成功并精确报告 | 5 包部分丢包 | 单包丢失即失败 | Komari 丢包率被放大 |

#### Ping 方案设计

**ICMP Ping 参数**：

| 参数 | XProbe 设计 | Nezha | Komari | 设计理由 |
|------|-----------|-------|--------|---------|
| 采样数 | **10 个包** | 5 | 1 | 10 包的标准误是 5 包的 1/√2 ≈ 71%，统计更稳定 |
| 发包间隔 | **0.5 秒** | 1 秒(默认) | - | 0.5 秒能在 5 秒内完成 10 包，比 Nezha 的 5 秒更快出结果 |
| 总超时 | **15 秒** | 20 秒 | 3 秒 | 10 包×0.5 秒=5 秒，15 秒超时留足余量 |
| DNS 解析 | **预解析排除** | 预解析 | 预解析 | 排除 DNS 时间，只测网络延迟 |
| 特权模式 | **优先 privileged**，降级 unprivileged | privileged | privileged | Linux 3.0+ 支持 unprivileged ICMP |

**延迟和丢包率计算**：

| 指标 | 计算方式 | 对比 Nezha |
|------|---------|-----------|
| **平均延迟** | 10 个包的 AvgRtt | 5 包平均，我们更稳定 |
| **最小延迟** | MinRtt | Nezha 不报告，我们额外提供 |
| **最大延迟** | MaxRtt | Nezha 不报告，我们额外提供 |
| **抖动(Jitter)** | StdDevRtt | Nezha 不报告，反映网络稳定性 |
| **丢包率** | `(PacketsSent - PacketsRecv) / PacketsSent × 100%` | 每次探测直接报告精确丢包率 |

**TCP Ping 参数**（ICMP 不可用时的降级方案）：

| 参数 | XProbe 设计 | Nezha | Komari |
|------|-----------|-------|--------|
| 采样数 | **5 次**取平均 | 1 次 | 1 次(有重试) |
| 超时 | **5 秒**/次 | 10 秒 | 3 秒 |
| 间隔 | **0.5 秒** | - | - |
| 延迟计算 | 5 次平均 | 单次 | 单次 |
| 丢包率 | 5 次中失败比例 | 成功/失败 | - |

**HTTP Ping 参数**：

| 参数 | XProbe 设计 | Nezha | Komari |
|------|-----------|-------|--------|
| 采样数 | **3 次**取平均 | 1 次 | 1 次 |
| 超时 | **10 秒**/次 | 30 秒 | 3 秒 |
| DNS 解析 | **排除**（自定义 DialContext） | 包含 | 排除 |
| 延迟计算 | 3 次平均 | 单次 | 单次 |
| 状态码判定 | 2xx-3xx 成功 | 2xx-399 | 2xx-3xx |

**关键改进点**（相比 Nezha 的优势）：

1. **每次探测直接报告精确丢包率**——发 10 个包，直接报告"10 包中收到 8 包，丢包率 20%"
2. **报告抖动(Jitter)**——StdDevRtt 反映延迟波动程度
3. **无偏差处理**——不采用 Komari 的高延迟重试策略，原样报告所有数据
4. **unprivileged ICMP 降级**——privileged（setcap）→ unprivileged（内核支持）→ TCP Ping

**unprivileged ICMP 前提（v1.3 新增）**：Linux unprivileged ICMP（`SOCK_DGRAM`）要求内核参数 `net.ipv4.ping_group_range` 覆盖 probe 用户组，**多数发行版默认禁用**。安装脚本写入 `/etc/sysctl.d/99-probe-agent.conf`（见 8.3）解决；若运行时仍未生效，自动降级 TCP Ping 并在面板标注实际方式。

**探测调度策略**：
- Ping 探测独立于常规监控数据上报，默认每 **60 秒**执行一轮完整探测
- 探测结果随下一次常规数据上报一起发送，或探测完成后立即单独上报
- 探测间隔可在服务端配置（管理员可调整为 30 秒或 120 秒）

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
写入 /etc/sysctl.d/99-probe-agent.conf          ← v1.3 新增
(net.ipv4.ping_group_range = 0 2147483647)       (解锁 unprivileged ICMP)
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

**systemd service 文件**：

```ini
[Unit]
Description=XProbe Agent
After=network.target

[Service]
Type=simple
User=probe
Group=probe
ExecStart=/usr/local/bin/probe-agent --config /etc/probe-agent/config.yml
Restart=always
RestartSec=5
# 安全加固
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ReadWritePaths=/etc/probe-agent /var/lib/probe-agent

[Install]
WantedBy=multi-user.target
```

---
## 5. Server 详细设计

### 5.1 REST API 端点

**v1.3 变更**：补齐删除服务器、指纹重置、Agent Token 重置、会话管理接口；注册接口增加限速；所有认证路由默认套全局限速中间件（默认 120 次/分钟/IP，可配置）。

| 方法 | 路径 | 认证 | 功能 | 安全要点 |
|------|------|------|------|---------|
| POST | `/api/v1/auth/login` | 无 | 登录 | 限速5次/分钟/IP, 连续10次失败锁定账号15分钟, 密码哈希验证 |
| POST | `/api/v1/auth/logout` | JWT | 登出 | 清除Cookie, 并吊销服务端会话记录（见 7.3） |
| GET | `/api/v1/auth/sessions` | JWT | 当前会话列表 | **v1.3 新增**；返回设备/IP/最近活跃, 供设置页展示 |
| DELETE | `/api/v1/auth/sessions/:id` | JWT | 吊销指定会话 | **v1.3 新增** |
| DELETE | `/api/v1/auth/sessions` | JWT | 吊销全部会话 | **v1.3 新增**；即"登出所有设备" |
| GET | `/api/v1/servers` | JWT | 服务器列表 | 支持标签/地区/关键词筛选参数 |
| GET | `/api/v1/servers/:id` | JWT | 单台详情(含历史) | - |
| GET | `/api/v1/servers/:id/history` | JWT | 历史趋势数据 | 按时间范围查询: `1h/6h/12h/1d/2d/7d/30d`（v1.3 新增 7d/30d, 读日聚合表） |
| DELETE | `/api/v1/servers/:id` | JWT | 删除服务器 | **v1.3 新增**；级联清理 metric_records / metric_records_daily / traffic_monthly / alert_history / 环形缓冲, 二次确认 |
| PUT | `/api/v1/servers/:id/meta` | JWT | 更新服务器元数据 | 名称/标签/位置/到期/成本等 |
| POST | `/api/v1/servers/:id/reset-fingerprint` | JWT | 重置主机指纹 | **v1.3 补齐**（v1.2 只在 7.5 提及未列入表）；重装系统/换网卡后由管理员调用 |
| POST | `/api/v1/agents/:id/reset-token` | JWT | 重置 Agent Token | **v1.3 新增**；旧 Token 立即失效, 新 Token 通过一次性展示交付, 管理员手动更新到 Agent 配置 |
| GET | `/api/v1/servers/:id/traffic` | JWT | 月度流量统计 | 按月查询收发累计 |
| GET | `/api/v1/tags` | JWT | 标签列表 | - |
| POST | `/api/v1/tags` | JWT | 创建标签 | 仅管理员 |
| PUT | `/api/v1/tags/:id` | JWT | 修改标签 | 仅管理员 |
| DELETE | `/api/v1/tags/:id` | JWT | 删除标签 | 仅管理员 |
| POST | `/api/v1/agent/register` | 注册码 | Agent注册 | 注册码一次性, 15分钟有效; **IP限速5次/分钟/IP（v1.3 新增）**; 主机指纹冲突返回409 |
| GET | `/api/v1/agent/config` | Agent Token (Authorization头) | 拉取探测目标配置 | 只读JSON, 无控制指令; Token禁止入URL |
| GET | `/api/v1/server-cert` | 无 | 获取Server证书SHA256指纹 | 公开信息; 供安装脚本写入Agent Pinning配置 |
| WS | `/api/v1/agent/report` | Agent Token (握手Authorization头) | Agent WebSocket接入 | 仅接受Agent→Server数据帧; 读限制64KB（v1.3） |
| GET | `/api/v1/alerts` | JWT | 告警规则列表 | - |
| POST | `/api/v1/alerts` | JWT | 创建告警规则 | 仅管理员 |
| PUT | `/api/v1/alerts/:id` | JWT | 修改告警规则 | 仅管理员 |
| DELETE | `/api/v1/alerts/:id` | JWT | 删除告警规则 | 仅管理员 |
| GET | `/api/v1/alerts/history` | JWT | 告警历史时间线 | FIRING/RESOLVED 记录, 供告警页展示 |
| GET | `/api/v1/notify/channels` | JWT | 通知渠道列表 | 敏感字段回显脱敏为 `***` |
| POST | `/api/v1/notify/channels` | JWT | 添加通知渠道 | SSRF校验URL |
| PUT | `/api/v1/notify/channels/:id` | JWT | 修改通知渠道 | SSRF校验URL |
| DELETE | `/api/v1/notify/channels/:id` | JWT | 删除通知渠道 | - |
| GET | `/api/v1/config/ping-targets` | JWT | 探测目标列表 | - |
| POST | `/api/v1/config/ping-targets` | JWT | 添加探测目标 | - |
| PUT | `/api/v1/config/ping-targets/:id` | JWT | 修改探测目标 | - |
| DELETE | `/api/v1/config/ping-targets/:id` | JWT | 删除探测目标 | - |
| GET | `/api/v1/config/settings` | JWT | 系统设置 | - |
| PUT | `/api/v1/config/settings` | JWT | 修改系统设置 | 仅管理员 |
| GET | `/api/v1/agents/tokens` | JWT | Agent Token列表 | 仅展示前8位+掩码, 不回显完整Token |
| POST | `/api/v1/agents/register-codes` | JWT | 生成注册码 | 最多5个未使用 |
| DELETE | `/api/v1/agents/register-codes/:id` | JWT | 删除注册码 | - |
| GET | `/api/v1/system/status` | JWT | 系统自身健康状态 | 供 8.8 自监控页使用 |
| GET | `/api/v1/public/:share_id` | 无 | 公开分享页 | share_id 为 UUIDv4 不可枚举; 只读, 无敏感信息 |
| GET | `/ws/dashboard` | JWT | 面板实时数据推送 | WebSocket; JWT 经 Cookie 校验 |

### 5.2 WebSocket 消息协议

**Agent → Server 帧**（仅这些类型，无其他；Token 在 WS 握手时经 `Authorization` 头校验，帧内不携带）：

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
- GET `/api/v1/server-cert` —— 获取 Server 证书指纹，供 Agent Pinning

**关键安全约束**：Server → Agent 的 WebSocket 方向**只有 1 种帧**（heartbeat_ack），不存在任何"下发配置""执行命令""建立会话""操作文件"的帧类型；配置变更一律由 Agent 定时 REST 拉取获得。这是 S1（纯只读架构）和 S4（无远程执行）的协议级保障。

**连接健壮性与传输细节（v1.3 新增）**：
- **读限制**：单帧最大 64KB（超出即断开连接并记录安全日志）；正常 report 帧 ≤10KB（见 7.6）
- **写超时**：10 秒写不进即断开，防慢速连接堆积
- **压缩**：可配置启用 permessage-deflate（gorilla/websocket `EnableCompression`），Agent 数量增长后显著节省带宽；默认开启，配置项 `monitor.ws_compression`
- **重连退避**：Agent 断线后指数退避 1s→2s→4s→…→60s 上限，**加 ±20% 随机 jitter**——避免 Server 重启后所有 Agent 同一时刻重连造成风暴
- **离线检测双路径**：Server 收到 WS close 事件**即时**将该 Agent 标记离线；90 秒心跳超时仅作为网络半开连接的兜底（v1.3：v1.2 仅依赖心跳超时，离线状态最长滞后 90 秒）

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
│  WS close即离线     │     │  ├ 网络 最近3600点    │
│  心跳超时兜底       │     │  └ Ping 最近60点     │
└───────────────────┘     └──────────────────────┘
        │                          │
        │                    每5分钟聚合
        │                          │
        │                          ▼
        │                  ┌──────────────┐
        │                  │ SQLite 落盘   │
        │                  │(5分钟聚合数据) │
        │                  └──────┬───────┘
        │                         │ 每日(UTC 00:00)聚合前一日
        │                         ▼
        │                  ┌──────────────┐
        └─────────────────▶│ 日聚合落盘    │
                           │(metric_records_daily)
                           └──────────────┘
        │
        ▼
┌───────────────────┐
│ 面板WebSocket推送   │
│ 浏览器实时数据      │
└───────────────────┘
```

- 环形缓冲大小：实时数据保留最近 3 小时（3 秒/点 × 3 小时 = **3600 点**，预分配 3600 点）——6H 视图由「3 小时实时缓冲 + 3 小时聚合数据」拼接而来（见 6.5）
- **5 分钟聚合**：每 5 分钟将当前实时数据聚合为一个点写入 `metric_records`，各字段规则：
  - CPU/内存/Swap/负载/连接数/进程数：取窗口内**平均值**
  - 磁盘使用率（JSON 按挂载点）：各挂载点分别取平均值，结构不变
  - 网络速率：取窗口内**平均速率**（缓冲中存的是速率而非累计字节）
  - Ping（JSON 按目标）：avg_latency 取均值，min/max 取极值，loss 与 jitter 取均值
- **日聚合（v1.3 新增）**：每日 UTC 00:00 将前一日全部 5 分钟点聚合为 `metric_records_daily` 一行（CPU/内存取 avg 与 max，网络取平均速率与峰值，Ping 取均值与最差丢包），支撑详情页 7d/30d 视图
- 历史数据保留：5 分钟聚合数据保留 90 天，**日聚合数据保留 365 天**（v1.3），超期自动清理
- 离线检测：WS close 即时置离线 + 90 秒心跳超时兜底（见 5.2）

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

**通知渠道 config JSON 示例**（`password`/`bot_token` 等敏感字段存储于 SQLite，API 回显时一律脱敏为 `***`）：

```json
{"type": "webhook", "url": "https://hooks.example.com/xxx"}
{"type": "telegram", "bot_token": "123456:ABC", "chat_id": "-100123456"}
{"type": "smtp", "host": "smtp.example.com", "port": 465, "username": "ops@b.c", "password": "***", "from": "ops@b.c", "to": ["oncall@b.c"]}
```

### 5.6 SQLite 表结构

**v1.3 变更**：`agents.token`/`register_codes.code` 改为仅存 SHA256 哈希（S9）；新增 `sessions`、`metric_records_daily` 表；`admin` 表移除 TOTP 字段（TOTP 移 v2，届时迁移添加）；GeoIP 坐标字段随地图功能移 v2。

```sql
-- Agent元数据
CREATE TABLE agents (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    token_hash TEXT UNIQUE NOT NULL,      -- SHA256(原始Token) hex, 不存原文(S9, v1.3)
    hostname TEXT NOT NULL,
    display_name TEXT,                    -- 自定义显示名称(NodeGet风格, 管理员设置)
    os TEXT,
    arch TEXT,
    agent_version TEXT,
    host_fingerprint TEXT,                -- SHA256(安装盐+CPU型号+主网卡MAC+系统类型), 见7.5
    ipv4 TEXT,                            -- 出口IPv4(Agent上报)
    ipv6 TEXT,                            -- 出口IPv6(Agent上报)
    region TEXT,                          -- 位置: "上海"/"Tokyo"/"US-LAX" (管理员设置)
    country_code TEXT,                    -- 国家代码: "CN"/"JP"/"US" (管理员设置, 用于旗帜; 地图定位在v2)
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

-- 月度流量归档(每Agent每月一行, 由Agent上报的累计值归档; 月界UTC)
CREATE TABLE traffic_monthly (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    agent_id INTEGER NOT NULL,
    month TEXT NOT NULL,                  -- "2026-08" (UTC月)
    rx_bytes INTEGER NOT NULL,            -- 当月最大累计接收字节数
    tx_bytes INTEGER NOT NULL,            -- 当月最大累计发送字节数
    UNIQUE(agent_id, month),
    FOREIGN KEY (agent_id) REFERENCES agents(id)
);

-- 注册码(仅存哈希, S9, v1.3)
CREATE TABLE register_codes (
    code_hash TEXT PRIMARY KEY,           -- SHA256(注册码) hex, 不存原文
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    used INTEGER DEFAULT 0,
    used_by_agent_id INTEGER
);

-- 告警规则
CREATE TABLE alert_rules (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    metric TEXT NOT NULL,                 -- cpu_usage/mem_usage/disk_usage/agent_offline/traffic_quota/expire_days
    operator TEXT NOT NULL,
    threshold REAL NOT NULL,
    duration INTEGER NOT NULL,
    enabled INTEGER DEFAULT 1,
    notify_channel_id INTEGER,
    created_at INTEGER NOT NULL
);

-- 告警历史(状态机持久化: Server重启后加载未RESOLVED记录恢复状态机, 避免重复通知)
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

-- 历史聚合数据(每5分钟一个点, 保留90天)
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

-- 日聚合数据(每日一行, 保留365天; 支撑详情页7d/30d视图, v1.3 新增)
CREATE TABLE metric_records_daily (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    agent_id INTEGER NOT NULL,
    date TEXT NOT NULL,                   -- "2026-08-31" (UTC日)
    cpu_usage_avg REAL,
    cpu_usage_max REAL,
    mem_usage_avg REAL,
    mem_usage_max REAL,
    disk_usage TEXT,                      -- JSON按挂载点取日均值
    net_rx_avg INTEGER,
    net_tx_avg INTEGER,
    ping_data TEXT,                       -- JSON按目标取日均值+最差丢包
    UNIQUE(agent_id, date),
    FOREIGN KEY (agent_id) REFERENCES agents(id)
);
CREATE INDEX idx_metric_daily_agent_date ON metric_records_daily(agent_id, date);

-- 管理员账户(TOTP字段随TOTP功能移v2, 届时迁移添加; v1.3)
CREATE TABLE admin (
    id INTEGER PRIMARY KEY,
    username TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,          -- bcrypt cost=12
    created_at INTEGER NOT NULL
);

-- 登录会话(JWT吊销依据, 支持列出/吊销/登出全部设备; v1.3 新增)
CREATE TABLE sessions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    token_hash TEXT UNIQUE NOT NULL,      -- SHA256(JWT) hex, 不存原文(S9)
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    revoked INTEGER DEFAULT 0,
    ip TEXT,                              -- 登录来源IP(会话列表展示)
    user_agent TEXT                       -- 设备描述(会话列表展示)
);
CREATE INDEX idx_sessions_expiry ON sessions(expires_at);

-- 公开分享页配置
CREATE TABLE share_pages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    share_id TEXT UNIQUE NOT NULL,        -- UUIDv4
    title TEXT,
    logo_url TEXT,                        -- 仅允许 https:// scheme(7.7, v1.3)
    footer_text TEXT,
    agent_ids TEXT,                       -- JSON数组: 公开的Agent ID白名单
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

### 5.7 地图视图与 GeoIP（已移入 v2）

**v1.3 变更**：原 GeoIP 离线解析（GeoLite2 mmdb + ECharts 世界地图 + effectScatter 光点）整章设计随地图视图移入 v2（见第 11 章），原因：
- GeoLite2 免费库需 MaxMind 注册审批，增加首次部署摩擦
- 地图是仪表盘三视图中最重的一块前端工作，v1 聚焦核心监控链路

v1 的处理方式：
- 仪表盘视图切换组件为卡片/表格两个视图 + 预留"地图"入口位（入口显示后隐藏或置灰提示 v2 提供）
- `agents.region` / `country_code` 手动字段保留（国旗展示与地区筛选依赖它们）
- 原方案中"管理员手动设置优先于 GeoIP 解析"的优先级设计、mmdb 缓存坐标到 `geo_lat`/`geo_lon` 字段等，均原样随 v2 路线图保留

**汇率折算**（到期成本多币种展示，v1 保留）：为避免 Server 对外请求汇率 API，采用管理员手动配置汇率（设置页可输入常用币种对 CNY 汇率），默认仅显示原始币种金额。

### 5.8 服务器元数据管理

NodeGet 风格的元数据字段（全部由管理员在面板设置，Agent 无法上报覆盖）：

| 字段 | 用途 | 前端展示 |
|------|------|---------|
| display_name | 自定义名称 | 卡片标题（替代 hostname） |
| region / country_code | 位置 | 卡片国旗 emoji + 城市名（v2 另用于地图定位） |
| isp | 供应商 | 卡片副标题 |
| tag_ids | 标签 | 卡片彩色标签徽章；顶部筛选栏 |
| expires_at | 到期时间 | 卡片剩余天数（30天内黄色、7天内红色）；到期告警 |
| price_* | 费用 | 卡片小字展示；汇总页合计（按币种分组，可选手动汇率折算） |
| traffic_quota_bytes | 流量配额 | 月流量进度条；超量告警 |

**新增告警指标**：`traffic_quota`（月流量超过配额 80%/100% 触发）、`expire_days`（到期前 30/7 天提醒）——复用现有告警引擎状态机，无新增攻击面。

---
## 6. 前端面板设计（NodeGet 风格 + v1.3 视觉升级）

> **设计基调**：对标 NodeGet 及其社区主题（NIE-Theme 等）的视觉风格——玻璃拟态卡片、圆环指标、延迟小格子、柔和渐变配色、多视图切换。整体气质：清爽、现代、信息密度适中、深浅色双主题。
>
> **v1.3 定调**：布局骨架与信息架构完全保留 NodeGet，视觉细节在 M0 设计系统阶段（见 9.2 M0）升级——不是像素级复刻，而是"比社区主题更高的完成度"：更严谨的色彩系统、双主题对比度达标、克制的动效、完整的空/加载态。

### 6.1 技术选型

| 维度 | 选择 | 理由 |
|------|------|------|
| 框架 | React 18 + TypeScript | 生态成熟，组件化模型适合主题/布局/分享功能 |
| 构建 | Vite | 快速构建，产物供 Go embed |
| UI 库 | shadcn/ui | 基于 Radix UI + Tailwind，语义 token 可定制性强；**图表外壳用 shadcn，图表实现用 ECharts**（shadcn Charts 包装的是 Recharts，与本方案冲突，忽略其 Chart 建议） |
| 图表 | ECharts | 性能好，支持实时数据流；地图能力留待 v2 |
| 状态管理 | Zustand | 轻量，适合中等复杂度应用 |
| 路由 | React Router | 标准选择 |
| 样式 | Tailwind CSS | 与 shadcn/ui 配套，主题切换方便（只用语义 token，禁写 raw 色值类） |
| 动效 | CSS transitions（150-300ms）+ ECharts 动画 | 轻量动效，不引入重型动画库；全局尊重 `prefers-reduced-motion` |
| i18n | 自研轻量 key-value 字典（v1.3 新增） | v1 仅中文，但字符串一律从组件抽离集中管理，为 v2 英文预留 |

### 6.2 设计语言（NodeGet 风格规范）

这是核心规范，开发时必须严格遵循：

#### M0 设计系统流程（v1.3 新增）

设计 tokens 定稿前按以下顺序执行（实现阶段由 AI 工具配合相应技能完成）：

1. **ui-ux-pro-max**：运行 `search.py "server monitoring dashboard dark glassmorphism" --design-system --persist`，产出设计系统基准（风格/配色/字体/反模式清单），持久化到 `docs/design-system/MASTER.md`
2. **frontend-design**：确定差异化美学方向（字体气质、背景纹理层次、玻璃质感参数），规避其明令禁止项（紫渐变+白底、千篇一律的字体选择）
3. **frontend-skill**：以"App/仪表盘章节"校准——克制、密而可读、少 accent 色、工具性文案（状态导向标题，非营销腔）
4. **shadcn**：将上述结论映射为语义 token（`--background/--foreground/--primary/--muted/--border` 等双主题变量），实现层以 shadcn 语义 token 规则为权威

#### 视觉基调

| 设计要素 | 规范 |
|---------|------|
| **卡片** | 玻璃拟态：半透明背景 + `backdrop-blur` + 细边框 + 大圆角（`rounded-2xl`）+ 柔和阴影 |
| **圆环指标** | CPU/内存/磁盘用 SVG 小圆环（SVG circle stroke-dashoffset），圆环中心显示百分比数字，圆环颜色随使用率渐变（绿→黄→红） |
| **延迟小格子** | VPS 圈经典组件：每行代表一个探测目标，行内 N 个小方格（最近 N 次探测），格子颜色按延迟分级渐变，**格子高度与延迟值成正比**（延迟越高格子越高），悬停显示具体数值与时间 |
| **延迟色阶** | 定义为 CSS 变量（`--lat-1` ~ `--lat-6`），**深浅主题各一套**（v1.3：浅色主题下必须通过 WCAG AA 4.5:1 对比度校验，验收标准之一）。基准色相：优(绿)→良(黄绿)→中(黄)→差(橙)→劣(红)→超时/离线(灰) |
| **状态色** | 成功/警告/危险同样语义 token 化，禁止组件内硬编码色值 |
| **字体** | 系统字体栈 + `tabular-nums`（数字等宽，图表对齐），中英文混排优化 |
| **深浅色** | 双主题默认跟随系统，CSS 变量切换；深色为主推主题（VPS 圈审美主流）。**v1.3：浅色主题背景规避"紫渐变+白底"的 AI 味配色**，使用中性冷灰或品牌色极浅渐变 |
| **背景** | 可切换：纯色 / 柔和渐变（默认，深色为深蓝紫系）/ 自定义图片 URL（仅 https，见 7.7）；背景之上叠加半透明卡片形成毛玻璃层次 |
| **间距** | 卡片网格 gap-4，卡片内 padding-5，信息分区用细分隔线或留白，不堆砌边框 |
| **空/加载态**（v1.3 新增） | 无服务器、无告警、无探测数据、Agent 离线、图表加载中等状态一律用 shadcn Skeleton/Empty 组件设计，禁止白屏或闪烁跳变 |

#### 交互与可访问性规范（v1.3 补充）

- 实时数据 3 秒推送，圆环和格子**平滑过渡动画**（CSS transition 300ms），数字变化不闪烁
- `prefers-reduced-motion: reduce` 时关闭推入动画与圆环动画（主题设置中的"圆环动画开关"默认跟随系统）
- 键盘可达：所有交互元素 `focus-visible` 可见；卡片支持 Enter 进入详情
- 触控目标 ≥44px；断点 375/768/1024/1440 全覆盖，无横向滚动（表格视图受控横向滚动除外）
- 卡片悬停轻微上浮 + 阴影加深（`hover:-translate-y-0.5`）
- 图表支持图例点击切换、悬停 tooltip 显示完整统计
- 移动端：卡片单列、顶部筛选收起为抽屉、表格视图受控横向滚动
- 页面不可见/窗口失焦/断网恢复时自动重新拉取数据（NodeGet 主题验证过的体验优化）
- 文案使用工具性语言（"在线 8/10""最近同步 12 秒前"），不用营销腔

### 6.3 页面结构

```
┌─────────────────────────────────────────────────────────┐
│  顶栏: Logo | 站点标题 | 搜索 | 主题切换 | 用户菜单       │
├─────────────────────────────────────────────────────────┤
│  主内容区（顶部导航标签，非侧边栏——NodeGet 风格）        │
│  ├ 仪表盘（卡片/表格 双视图, 预留地图入口位）             │
│  ├ 告警                                                │
│  ├ 通知                                                │
│  ├ 设置                                                │
│  └ 公开页配置                                          │
└─────────────────────────────────────────────────────────┘
```

### 6.4 仪表盘（Dashboard）——双视图 + 筛选栏

#### 6.4.1 顶部筛选栏（所有视图共享）

```
┌──────────────────────────────────────────────────────────────┐
│ 概览条: 在线 8/10 · 离线 2 · 平均CPU 45% · 平均内存 60%       │
│        本月总流量 1.2TB · 月成本合计 $86.50 / ¥4200          │
├──────────────────────────────────────────────────────────────┤
│ [搜索🔍 服务器名/标签/位置] [标签▾] [地区▾] [IPv4/IPv6/全部▾]│
│ 排序[名称/CPU/内存/流量/延迟▾]        视图: [卡片] [表格] (地图v2)│
└──────────────────────────────────────────────────────────────┘
```

- 筛选条件组合生效，URL 参数持久化（刷新/分享保持筛选状态）
- 标签筛选显示彩色标签徽章；地区筛选按 country_code 分组（含国旗）
- 排序支持：名称 / CPU / 内存 / 磁盘 / 月流量 / 平均延迟 / 到期时间
- 视图切换组件预留"地图"入口位（v1 置灰 + tooltip"v2 提供"，v1.3 变更）

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
| 指标区 | CPU/内存/磁盘三个**小圆环**（64px），圆环中心大号百分比，圆环下方小字标签；CPU 首采样显示 `--` | RingProgress 组件，颜色随阈值渐变 |
| 流量区 | 实时上下行速度（↑↓图标+数值）+ **月流量进度条**（当月累计/配额，超 80% 变橙、100% 变红，无配额只显示数值） | 细进度条 |
| 延迟区 | **延迟小格子图**：默认展示 IPv4 全部线路（按 isp→region 自动分组命名，如"上海电信"），每行一个目标，展示最近 24 次探测的小格子；IPv6 目标在无 IPv6 出口时显示"无v6" | LatencyGrid 组件（见 6.4.4） |
| 尾部 | 到期时间（30天内黄/7天内红+⚠）+ 费用（币种符号+金额/周期） | 小字灰色 |

- 离线卡片：整体降低不透明度，圆环显示灰色 `--`，延迟格子全灰
- 卡片整体可点击进入详情页（尾部到期/费用区域可 hover 显示编辑入口）

#### 6.4.3 表格视图

紧凑列表，适合服务器多的场景（一屏看全所有机器）：

延迟/丢包列**按实际探测目标动态生成**（不硬编码"电信/联通/移动"，国际部署可自定义目标名）：

| 名称 | 状态 | 位置 | CPU | 内存 | 磁盘 | 上下行 | 月流量 | 电信 | 移动v6 | 到期 |
|------|------|------|-----|------|------|--------|--------|------|--------|------|
| 🇺🇸 US-LAX-01 | ● | 洛杉矶 | 45% | 60% | 70% | 1.2/3.5M | 320G/1T | 12ms | 155ms | 178天 |
| 🇭🇰 HK-家宽-02 | ● | 香港 | 12% | 30% | 50% | 0.5/1.0M | 110G/500G | 42ms | 无v6 | 6天⚠ |

- 延迟列 = 启用的探测目标（列头显示 target name），目标多时表格受控横向滚动；IPv6 目标带 `v6` 徽标
- CPU/内存/磁盘列用迷你进度条（单元格内 60px 宽）
- 延迟列显示当前平均延迟（带颜色分级小圆点），Agent 无 IPv6 出口显示"无v6"
- 行点击进详情，表头点击排序

#### 6.4.4 延迟小格子组件（LatencyGrid，核心组件）

```
单个格子: 颜色 = 延迟分级, 高度 = 延迟数值映射

 上海电信   ▁▂▁▁▂▁▁▁▂▁▁▁▁▂▁▁▁▁▁▂▁▁      当前: 12.5ms 丢包0%
 上海联通   ▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁      当前:  8.3ms 丢包0%
 上海移动   ▂▄▂▂▄▂▂▂▄▂▂▂▂▄▂▂▂▂▂▄▂▂      当前: 15.2ms 丢包10%

格子颜色分级 (CSS 变量, 深浅主题各一套, v1.3):
  --lat-1  <50ms   绿
  --lat-2  <100ms  黄绿
  --lat-3  <200ms  黄
  --lat-4  <400ms  橙
  --lat-5  ≥400ms  红
  --lat-6  超时/失败 灰
  (色值在 M0 设计系统定稿; 浅色主题必须通过 4.5:1 对比度校验)
格子高度: 按该行数据归一化(最小格子4px, 最高20px), 延迟越高格子越高
```

- 每行 = 一个探测目标（按 isp→region 分组命名），行内格子 = 最近 N 次探测（默认 24 次，即 24 分钟）
- 悬停单个格子：tooltip 显示该次探测的「时间 · 平均/最小/最大/抖动 · 丢包率」
- 行尾显示当前值（平均延迟 + 丢包率）
- 数据源：环形缓冲中的 Ping 历史（每 60 秒一个点，保留最近 1 小时 60 点）
- 新格子从右侧推入时带 300ms 平滑动画（`prefers-reduced-motion` 时关闭）

### 6.5 服务器详情（Server Detail）

```
┌──────────────────────────────────────────────────────────────┐
│ ← 返回 | 🇺🇸 US-LAX-01 [生产][中转] ● 在线 | Linux x86_64 | 3天│
│    洛杉矶 · Bandwagon · IPv4: 1.2.3.4 · IPv6: 2600:...       │
├──────────────────────────────────────────────────────────────┤
│  时间范围: [1小时] [6小时] [12小时] [1天] [2天] [7天] [30天]  │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐ │
│  │ 延迟小格子图 (IPv4)                                     │ │
│  │  上海电信 ▁▂▁▁▂▁▁▁▂▁▁▁▁▂▁▁▁▁▁▂▁▁ (每行一条线路)       │ │
│  │  上海联通 ▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁                       │ │
│  │  上海移动 ▂▄▂▂▄▂▂▂▄▂▂▂▂▄▂▂▂▂▂▄▂▂                       │ │
│  ├────────────────────────────────────────────────────────┤ │
│  │ 延迟小格子图 (IPv6, 如有v6目标)                         │ │
│  │  上海电信v6 ▁▁▂▁▁▁▂▁▁▁▂▁▁▂▁▁▁▂▁▁▁                      │ │
│  └────────────────────────────────────────────────────────┘ │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐ │
│  │ CPU 占用率 (实时折线图)                                  │ │
│  │  当前: 45% | 平均: 42% | 最大: 78%                       │ │
│  │  负载: 0.52 / 0.48 / 0.50 (1/5/15分钟)                  │ │
│  └────────────────────────────────────────────────────────┘ │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐ │
│  │ 内存占用率 (实时折线图)                                  │ │
│  │      当前: 60% | 总量: 8GB | Swap: 0%                    │ │
│  └────────────────────────────────────────────────────────┘ │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐ │
│  │ 延迟与丢包率融合图 (实时折线图)                           │ │
│  │  延迟: 按目标多条折线(区分v4/v6)                          │ │
│  │  丢包率: 对应柱状(与延迟图共享X轴对齐)                    │ │
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
- 保留 CPU/内存实时折线图和延迟丢包融合图（时间范围 7 档切换）
- 月流量统计卡片：当月累计/配额进度条 + 最近 12 个月月度流量柱状图（历史归档数据）
- 信息与费用卡片：系统信息 + 到期/费用（管理员可点击编辑）

**时间范围切换数据源**（v1.3 新增 7d/30d 档）：

| 时间范围 | 数据源 | 数据粒度 | 刷新方式 |
|---------|--------|---------|---------|
| 1 小时 | 环形缓冲 | 3 秒/点 | WebSocket 实时 |
| 6 小时 | 环形缓冲 + SQLite 聚合 | 3 秒/点 + 5 分钟/点 | WebSocket 实时 |
| 12 小时 | metric_records | 5 分钟/点 | 定时轮询(5分钟) |
| 1 天 | metric_records | 5 分钟/点 | 定时轮询(5分钟) |
| 2 天 | metric_records | 5 分钟/点 | 定时轮询(5分钟) |
| **7 天**（v1.3） | metric_records_daily | 1 天/点 | 定时轮询(30分钟) |
| **30 天**（v1.3） | metric_records_daily | 1 天/点 | 定时轮询(30分钟) |

（延迟格子图固定使用环形缓冲最近 60 分钟数据，不随时间范围切换变化）

### 6.6 其他页面

**告警管理**：告警规则列表（CRUD，含流量配额/到期提醒规则）+ 告警历史记录（FIRING/RESOLVED 时间线）

**通知渠道**：Webhook/Telegram/邮件渠道管理，支持测试发送

**设置**：
- 账户安全（修改密码；v1 无 TOTP——TOTP 移 v2；登录限速状态展示）
- **会话管理**（v1.3 新增）：当前登录会话列表（设备/IP/最近活跃），支持吊销单个会话与"登出所有设备"
- 探测目标管理（目标列表 + region/isp/ip_version 元数据编辑）
- 服务器管理：Agent 列表（注册码/Token 重置/一键安装命令）+ **元数据编辑**（显示名称/位置/国旗/标签/到期/费用/流量配额）+ 删除服务器（二次确认）
- 标签管理（创建/改名/颜色）
- 系统设置（数据保留/Ping 间隔/静默期/汇率配置/WS 压缩开关）
- 外观（主题/背景/卡片风格/强调色）
- 公开页配置（标题/Logo/页脚/包含的服务器/展示字段开关）

**公开分享页**（`/s/:share_id`，无需登录）：与仪表盘同款 UI（NodeGet StatusShow 定位），但：
- 仅展示管理员勾选的 Agent，仅展示非敏感字段（无 IP 详情、无 Token、无配置）
- 可配置站点标题/Logo/页脚（存 share_pages 表）；**logo_url 仅允许 https:// scheme**（v1.3：防 `data:`/`javascript:` 类 SVG XSS，见 7.7）
- 支持卡片/表格双视图与筛选（仅对公开的 Agent 生效；v1 无地图视图）
- 无登录入口暴露（登录页路径不对外泄露，仅管理员知晓）

### 6.7 主题系统

```
主题切换流程:
用户选择 ──▶ localStorage存储 ──▶ CSS变量切换 ──▶ shadcn/ui组件自动适配

外观配置项(设置页 + 公开页各自独立):
├ 模式: 浅色 (Light) / 深色 (Dark) / 跟随系统 (System)  [默认: 跟随系统]
├ 背景: 纯色 / 柔和渐变(默认深蓝紫) / 自定义图片URL(仅https)
├ 卡片风格: 玻璃拟态(默认) / 经典实底 / 极简描边
├ 强调色: 预设8色(蓝/绿/紫/粉/橙/青/玫瑰/石板) + 自定义色值
└ 圆环动画: 开/关/跟随系统reduced-motion(默认跟随系统)
```

### 6.8 实时数据 WebSocket 流

```
浏览器 ──▶ WebSocket连接 /ws/dashboard (JWT经Cookie认证)
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
| T2 | 攻击者截获 Agent-Server 通信 | 同网段嗅探明文通信 | 是 | S2：强制 TLS + 证书 Pinning，Agent 拒绝明文连接 |
| T3 | 攻击者利用多租户授权缺陷越权 | BOLA/IDOR 访问他人资源 | 是 | S5：第一版单管理员，无多租户攻击面 |
| T4 | 攻击者通过 Webhook 通知发起 SSRF | 通知 URL 指向内网，反射响应体 | 是 | S6：SSRF 防护层 |
| T5 | 攻击者暴力破解管理员密码 | 自动化登录尝试 | 通用威胁 | 登录限速 + 账号锁定；v2 追加 TOTP |
| T6 | 攻击者窃取 Agent Token 后在其他机器使用 | 复制配置文件到另一台机器 | 通用威胁 | 主机指纹绑定 |
| T7 | 攻击者伪造 Agent 上报数据 | 用有效 Token 提交伪造数据 | 是 | 数据合理性校验 + 上报频率限制 |
| T8 | 攻击者通过 CSRF 触发状态变更 | 诱导已登录用户访问恶意链接 | 是 | JWT 存储在 HttpOnly Cookie + SameSite=Strict |
| T9 | 攻击者利用注册码重复注册 | 截获一键安装命令后重复使用 | 通用威胁 | 注册码一次性 + 15 分钟有效期 + 接口限速 |
| T10 | Agent 以 root 运行导致 RCE 危害放大 | 即使不存在 RCE，root 运行增加风险 | 是 | S3：非 root + 最小 capabilities |
| T11 | 攻击者通过公开分享页泄露敏感信息 | 分享页暴露 IP/Token/配置 | 通用威胁 | 分享页白名单字段，仅展示非敏感信息 |
| T12 | 攻击者利用 WebSocket 端点未授权访问 | 直接连接 dashboard WS 获取所有数据 | 是 | WS 端点强制 JWT 认证 |
| **T13**（v1.3 新增） | 攻击者获取数据库文件（备份/快照/误配置） | 直接读取 agents.token 等凭证 | 通用威胁 | S9：Token/注册码仅存 SHA256 哈希，泄露文件不含可用凭证 |
| **T14**（v1.3 新增） | 通过分享页自定义字段注入（XSS/恶意跳转） | logo_url 填 `javascript:`/`data:` URL，页脚注入脚本 | 通用威胁 | logo_url 仅允许 https scheme + React 转义 + CSP（见 7.7） |

### 7.2 S1 + S4：纯只读架构（根除 RCE）

**协议层**：WebSocket 消息协议中，Server→Agent 方向只有 1 种帧（heartbeat_ack 确认），不存在"命令"帧。

**代码层**：Agent 代码中不引入任何命令执行相关功能：
- 不调用 `os/exec`、`syscall.Exec`
- 不建立 PTY/Shell 会话
- 不提供文件系统操作接口
- 不包含 Cron 任务执行器
- GPU 采集用 dlopen 直读 NVML，不调用 nvidia-smi 等外部命令

**架构层**：Server 到 Agent 方向不存在主动连接。所有通信由 Agent 发起。

**CI 自动验证（v1.3 新增）**：S4 从"人工审计"升级为"每次构建可验证"，CI 流水线包含：
- `noexec` 检查 job：对 Agent 源码/二进制检查 `os/exec`、`exec.Command`、`pty` 等符号，**零匹配才允许合并/发布**（实现为 `make audit-noexec`）
- `gosec`：Go 静态安全扫描
- `gitleaks`：提交内容密钥泄露扫描

### 7.3 S5：单管理员 + 强认证

**JWT 安全配置**：

| 配置项 | 值 | 理由 |
|--------|-----|------|
| 存储 | HttpOnly Cookie | 防止 JS 读取（防 XSS 窃取） |
| SameSite | Strict | 防 CSRF（比 Nezha 的 Lax 更严格） |
| Secure | true | 仅 HTTPS 传输 |
| 有效期 | **2 小时 + 静默续期**（v1.3 统一：v1.2 的 7.3 写 12h 与 8.2 写 2h 矛盾，现统一为 2h） | 短有效期缩小窗口；前端在剩余 30 分钟内发起请求时自动续期 |
| 签名算法 | HS256 | 对称签名，单服务端够用 |
| 密钥 | 随机生成 32 字节 | 首次启动生成存配置文件；**支持环境变量注入**（Docker 部署推荐 `PROBE_JWT_SECRET`，v1.3） |
| 吊销 | sessions 表 | 每个会话可单独吊销（见 7.3.2） |

**密码策略**：
- 首次启动强制设置密码（无默认密码，不像 Nezha 默认 admin/admin）
- 最小长度 12 位
- 必须包含大小写字母 + 数字
- bcrypt cost = 12

**7.3.1 密码找回（v1.3 新增）**：单管理员系统无"忘记密码"邮件通道，提供服务器端 CLI：

```bash
probe-server reset-password --username admin   # 交互输入新密码
# 重置成功同时吊销该用户全部会话
```

**7.3.2 会话管理（v1.3 新增）**：JWT 吊销基于 sessions 表（见 5.6）：
- 登录成功创建会话记录（token 哈希/IP/User-Agent/过期时间）
- 设置页"会话管理"：列出当前全部会话，可吊销单个或"登出所有设备"
- 认证中间件校验 JWT 时同步校验对应会话未被吊销且未过期
- 清理任务定时删除已过期/已吊销超 7 天的会话记录

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

- 注册时上报主机指纹，Server 绑定 Agent 与指纹（UNIQUE 约束：同指纹仅允许一台在线）
- 后续连接时 Server 校验指纹，不匹配则拒绝并记录安全日志
- 指纹变更（更换网卡/重装系统）→ 面板调用 `POST /api/v1/servers/:id/reset-fingerprint` 重置（v1.3 已补入 API 表），Agent 下次注册时重新绑定
- 冲突处理：注册时指纹已存在（同模板批量 VPS 的极端撞车）→ 返回 409 指纹冲突，管理员选择「删除旧记录」或「重置旧记录指纹」后重试
- **Token 重置（v1.3 新增）**：`POST /api/v1/agents/:id/reset-token` 吊销旧 Token 并签发新 Token（新 Token 仅在响应中完整展示一次，服务端只存哈希）；管理员手动更新到 Agent 配置
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
| 数据大小 | 单次上报 ≤ 10KB | 超大拒绝；**WS 层单帧 >64KB 直接断开（v1.3 落实传输层限制）** |

### 7.7 安全响应头、CORS 与输入校验（v1.3 新增）

**安全响应头中间件**（全部响应统一附加）：

| 响应头 | 值 | 说明 |
|--------|-----|------|
| Strict-Transport-Security | `max-age=31536000; includeSubDomains` | 强制 HTTPS 记忆 |
| Content-Security-Policy | `default-src 'self'; img-src 'self' https: data:; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'; frame-ancestors 'none'` | script 无 inline（打包产物）；style 保留 'unsafe-inline'（Tailwind/ECharts 运行时样式）；img 允许 https（分享页 logo_url） |
| X-Frame-Options | `DENY` | 防点击劫持 |
| X-Content-Type-Options | `nosniff` | 防 MIME 嗅探 |
| Referrer-Policy | `strict-origin-when-cross-origin` | 收窄 referrer 泄露 |

**CORS 策略**：默认**同源无 CORS**（前端 embed 于 Server）；开发模式仅白名单 `http://localhost:5173`（Vite dev server）。生产环境不提供任何 CORS 放宽配置项——避免被误配置打开。

**分享页输入校验**：`logo_url` 仅允许 `https://` scheme（拒绝 `data:`/`javascript:`/相对路径外链）；`title`/`footer_text` 限长并由 React 默认转义渲染，禁止 `dangerouslySetInnerHTML`。

### 7.8 安全审计清单

开发完成后，按此清单审计（**v1.3 起清单中标注 ⚙ 的项由 CI 自动检查**）：

- [ ] ⚙ 全局搜索 `os/exec`、`exec.Command`，确认零匹配（S4；CI noexec job）
- [ ] ⚙ Agent 侧搜索 `pty`、`shell`、`terminal`，确认零匹配（S4；CI noexec job）
- [ ] ⚙ gitleaks 扫描通过（无密钥入库）
- [ ] ⚙ gosec 扫描无 High/Critical
- [ ] 确认所有 WebSocket 帧类型有明确定义，无"通用命令"帧（S1）
- [ ] 确认 TLS 配置不可关闭，Agent 拒绝明文连接（S2）
- [ ] 确认 JWT Cookie 配置为 HttpOnly + Secure + SameSite=Strict，有效期 2h + 静默续期（S5）
- [ ] 确认登录限速生效（S5）
- [ ] 确认注册接口限速生效（v1.3）
- [ ] 确认全局 API 限速中间件生效（v1.3）
- [ ] 确认 Webhook 通知经过 SSRF 防护（S6）
- [ ] 确认 Agent systemd service 以 probe 用户运行（S3）
- [ ] 确认配置文件权限 600（S8）
- [ ] 确认 agents.token / register_codes / sessions 仅存哈希（S9；v1.3）
- [ ] 确认安全响应头（HSTS/CSP/XFO/nosniff/Referrer-Policy）全部下发（v1.3）
- [ ] 确认分享页 logo_url scheme 白名单生效（v1.3）
- [ ] 确认 WS 读限制 64KB 与写超时生效（v1.3）
- [ ] 确认会话列出/吊销功能生效（v1.3）
- [ ] 确认注册码一次性 + 15 分钟有效期（T9）
- [ ] 确认主机指纹校验生效（T6）
- [ ] 确认数据合理性校验生效（T7）
- [ ] 确认公开分享页不包含敏感信息（T11）
- [ ] 确认所有 WebSocket 端点需要认证（T12）
- [ ] 确认删除 Agent 级联清理全部关联数据（v1.3）

---

## 8. 部署与运维

### 8.1 Server 部署方式

#### Docker 部署（推荐）

```yaml
version: '3.8'
services:
  probe-server:
    image: xprobe-server:latest
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
      - PROBE_JWT_SECRET=${PROBE_JWT_SECRET}   # v1.3: 建议环境变量注入, 覆盖配置文件
    read_only: true
    tmpfs:
      - /tmp
    security_opt:
      - no-new-privileges:true
    cap_drop:
      - ALL
```

#### 二进制部署

```bash
curl -fsSL https://your-server.com/install-server.sh | bash
```

### 8.2 Server 配置文件

```yaml
listen: ":443"
data_dir: "/var/lib/probe-server"

tls:
  cert: "/etc/probe-server/cert.pem"
  key: "/etc/probe-server/key.pem"

auth:
  jwt_secret: ""                       # 留空: 首次启动自动生成32字节随机值并回写本文件;
                                       # 也可用环境变量 PROBE_JWT_SECRET 注入(v1.3, Docker推荐), 切勿手填弱值
  jwt_expiry: "2h"                     # 短有效期 + 前端静默续期(见7.3)
  login_rate_limit: 5                  # 次/分钟/IP; 连续10次失败锁定账号15分钟
  register_rate_limit: 5               # Agent注册接口 次/分钟/IP(v1.3 新增)
  global_rate_limit: 120               # 全局API 次/分钟/IP(v1.3 新增)

monitor:
  report_interval: 3
  heartbeat_timeout: 90
  ring_buffer_size: 3600               # 3秒/点 × 3小时 = 3600点(见5.3)
  aggregation_interval: 300
  ws_compression: true                 # permessage-deflate(v1.3 新增)

storage:
  metric_retention_days: 90            # 5分钟聚合保留
  daily_retention_days: 365            # 日聚合保留(v1.3 新增)
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

**v1.3 变更**：补齐 install_salt 生成、证书指纹带外获取、`ping_group_range` 内核参数、状态目录属主（对齐 10.8 M6 提示词）；二进制从 Server 内嵌分发端点下载。

```bash
#!/bin/bash
# install-agent.sh (由 Server 提供并内嵌于面板一键命令)
set -euo pipefail

# 1. 解析参数
SERVER_URL=""
REGISTER_CODE=""
CERT_FP=""                               # 可选: 手动指定证书指纹(自签场景带外传递)
while [[ $# -gt 0 ]]; do
    case $1 in
        --server) SERVER_URL="$2"; shift 2;;
        --code) REGISTER_CODE="$2"; shift 2;;
        --cert-fingerprint) CERT_FP="$2"; shift 2;;
        *) echo "Unknown: $1"; exit 1;;
    esac
done
[[ -z "$SERVER_URL" || -z "$REGISTER_CODE" ]] && { echo "需要 --server 与 --code"; exit 1; }

# 2. 检测系统 (v1 仅 Linux)
ARCH=$(uname -m)
case $ARCH in
    x86_64)  ARCH="amd64";;
    aarch64) ARCH="arm64";;
    armv7l)  ARCH="armv7";;
    *) echo "Unsupported arch: $ARCH"; exit 1;;
esac

# 3. 下载二进制(Server 内嵌分发)并校验SHA256
AGENT_URL="${SERVER_URL}/download/agent/linux/${ARCH}"
curl -fsSL -o /tmp/probe-agent "${AGENT_URL}"
curl -fsSL -o /tmp/probe-agent.sha256 "${AGENT_URL}.sha256"
echo "$(cat /tmp/probe-agent.sha256)  /tmp/probe-agent" | sha256sum -c -
chmod +x /tmp/probe-agent
mv /tmp/probe-agent /usr/local/bin/probe-agent

# 4. 创建用户与目录
id probe &>/dev/null || useradd -r -s /usr/sbin/nologin probe
mkdir -p /etc/probe-agent /var/lib/probe-agent

# 5. 生成安装盐(参与主机指纹计算, 升级/重装时保持不变则指纹不变)
INSTALL_SALT=$(openssl rand -hex 32)

# 6. 带外获取 Server 证书指纹(写入 Agent Pinning 配置)
if [[ -z "$CERT_FP" ]]; then
    CERT_FP=$(curl -fsSL "${SERVER_URL}/api/v1/server-cert" | grep -o '"fingerprint"[[:space:]]*:[[:space:]]*"[^"]*"' | cut -d'"' -f4 || true)
fi

# 7. 解锁 unprivileged ICMP (v1.3: 否则降级链第二级在多数发行版直接失败)
echo 'net.ipv4.ping_group_range = 0 2147483647' > /etc/sysctl.d/99-probe-agent.conf
sysctl --system >/dev/null

# 8. 写配置(权限600, 属主probe)
cat > /etc/probe-agent/config.yml << EOF
server: "${SERVER_URL}"
register_code: "${REGISTER_CODE}"
install_salt: "${INSTALL_SALT}"
server_cert_fingerprints:
  - "${CERT_FP}"
state_file: "/var/lib/probe-agent/state.json"
report_interval: 3
config_sync_interval: 3600
ping_method: "auto"
EOF
chown -R probe:probe /etc/probe-agent /var/lib/probe-agent
chmod 600 /etc/probe-agent/config.yml

# 9. 尝试 setcap (ICMP Ping)
if setcap cap_net_raw+ep /usr/local/bin/probe-agent 2>/dev/null; then
    echo "ICMP Ping enabled (CAP_NET_RAW)"
else
    echo "setcap failed, will try unprivileged ICMP or fallback to TCP Ping"
fi

# 10. 安装 systemd service
cat > /etc/systemd/system/probe-agent.service << 'EOF'
[Unit]
Description=XProbe Agent
After=network.target

[Service]
Type=simple
User=probe
Group=probe
ExecStart=/usr/local/bin/probe-agent --config /etc/probe-agent/config.yml
Restart=always
RestartSec=5
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ReadWritePaths=/etc/probe-agent /var/lib/probe-agent

[Install]
WantedBy=multi-user.target
EOF

# 11. 启动
systemctl daemon-reload
systemctl enable probe-agent
systemctl start probe-agent

echo "Agent installed and started."
echo "Check status: systemctl status probe-agent"
```

### 8.4 版本发布流程

**v1.3 变更**：新增"Agent 二进制内嵌"构建步骤；CI 附安全检查。

```
1. 更新版本号 (VERSION文件)
2. 构建前端 (cd frontend && npm run build → web/)
3. 交叉编译 Agent: linux-amd64, linux-arm64, linux-armv7 (纯Go, 无cgo, 单命令可编译)
4. 将 Agent 二进制放入 server/assets/agents/{linux-amd64,linux-arm64,linux-armv7}/
5. 构建嵌入 Agent 二进制的 Server: linux-amd64, linux-arm64
6. 生成全部二进制的SHA256校验文件
7. CI 安全检查: noexec(零匹配) + gosec + gitleaks → 任一失败终止发布
8. 构建Docker镜像 (linux/amd64, linux/arm64)
9. 发布 GitHub Release(Server二进制自包含; 同时附 Agent 二进制供直接下载场景)
```

### 8.5 升级方案

**Server 升级**：停止服务 → 替换二进制 → 启动服务（SQLite 自动迁移）

**Agent 升级**：手动升级（第一版不做自动升级，因为自动升级违反纯只读架构）

**注意**：Agent 自动升级意味着 Server 能向 Agent 下发新二进制并执行，这违反了纯只读架构（S1+S4）。第一版只做手动升级，面板提示哪些 Agent 需要升级。Server 向下兼容旧版 Agent 的上报格式。升级脚本流程：停止服务 → 备份旧版 → 替换新版 → 重新 setcap → 启动服务；**升级不动配置与 state.json**（install_salt 不变则主机指纹不变，避免误触发 409）。

### 8.6 备份与恢复

**v1.3 修正**：WAL 模式下直接 `cp` 数据库文件可能得到不一致快照，改用 SQLite 在线备份 API：

```bash
# 备份(在线一致性快照)
sqlite3 /var/lib/probe-server/probe.db ".backup '/backup/xprobe-$(date +%Y%m%d).db'"
# (等价: VACUUM INTO '/backup/xprobe.db'; 大规模部署可选 litestream 持续复制)

# 恢复
systemctl stop probe-server
cp /backup/xprobe-20260621.db /var/lib/probe-server/probe.db
systemctl start probe-server
```

备份文件中不含任何明文凭证（S9：Token/注册码/会话均存哈希）。

### 8.7 日志设计

```
日志级别: DEBUG / INFO / WARN / ERROR

日志格式 (JSON):
{"time":"2026-06-21T10:00:00Z","level":"INFO","module":"monitor","msg":"agent connected","agent_id":1}
{"time":"2026-06-21T10:05:00Z","level":"WARN","module":"monitor","msg":"agent heartbeat timeout","agent_id":3}
{"time":"2026-06-21T10:10:00Z","level":"ERROR","module":"notify","msg":"webhook send failed","error":"timeout"}

安全相关日志 (独立记录):
- 登录失败/限速触发
- 主机指纹不匹配
- SSRF 阻断
- 注册码使用
- WS 帧超限断开(v1.3)
- 会话吊销(v1.3)
```

### 8.8 监控自身健康

Server 面板"系统状态"页面（仅管理员，`GET /api/v1/system/status`）：

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

**v1.3 变更**：新增 M0 设计系统里程碑；M3 因移除地图/GeoIP 缩短；M5 因移除 TOTP 缩短。

```
M0: 设计系统与视觉基线 (0.5周)
    │
    ▼
M1: 基础架构 + 核心采集 (2周)
    │
    ▼
M2: Agent-Server 通信 + 注册上线 (2周)
    │
    ▼
M3: 实时监控 + 前端面板 (3周)
    │
    ▼
M4: 网络探测(Ping) + 历史数据 (2周)
    │
    ▼
M5: 告警 + 通知 + 安全加固 (1.5周)
    │
    ▼
M6: 部署 + 发布 + 文档 (1周)
```

### 9.2 各里程碑详细任务

#### M0: 设计系统与视觉基线（0.5 周，v1.3 新增）

**目标**：在写任何 UI 代码前定稿设计 tokens，避免实现期风格漂移。

| 任务 | 类型 | 说明 |
|------|------|------|
| 设计系统检索 | 设计 | ui-ux-pro-max `search.py "server monitoring dashboard dark glassmorphism" --design-system --persist`，产出基准并持久化 `docs/design-system/MASTER.md` |
| 差异化定调 | 设计 | 依 frontend-design 原则确定字体气质/背景纹理/玻璃质感参数；规避紫渐变+白底等禁忌 |
| 密度校准 | 设计 | 依 frontend-skill"App UI 克制"章节校准信息密度、accent 数量、工具性文案基调 |
| tokens 落地 | 配置 | 映射为 shadcn 语义 token（双主题 CSS 变量表）：延迟色阶 --lat-1~6、状态色、字体栈、间距/圆角/阴影/动效时长 |
| 可访问性基线 | 设计 | 双主题对比度 ≥4.5:1 校验、focus-visible、44px 触控、375-1440 断点清单 |

**验收标准**：
- 双主题色阶对比度校验通过（含延迟色阶）
- `docs/design-system/MASTER.md` 与 Tailwind/shadcn token 配置草案完成
- 核心组件视觉规格确认：GlassCard / RingProgress / LatencyGrid / MiniBar

#### M1: 基础架构 + 核心采集（2 周）

**目标**：Agent 能采集系统指标，Server 能存储数据。

| 任务 | 类型 | 说明 |
|------|------|------|
| 项目初始化 | 代码 | Go module 初始化，目录结构搭建，共享 model 包 |
| 采集器：CPU | TDD | 读 `/proc/stat`，计算使用率，写测试验证计算逻辑；**首采样置空（null）**（v1.3） |
| 采集器：内存 | TDD | 读 `/proc/meminfo`，解析 MemTotal/MemAvailable |
| 采集器：磁盘 | TDD | 系统调用 statfs，遍历挂载点 |
| 采集器：网络 | TDD | 读 `/proc/net/dev`，计算速率差值 |
| 采集器：系统信息 | TDD | uname 系统调用，运行时间，进程数 |
| 状态持久化 | TDD | state.json 读写，流量累计跨重启续算，UTC 月界（v1.3） |
| SQLite 数据层 | TDD | 建表（原生 SQL），WAL 模式，CRUD 测试 |
| 环形缓冲 | TDD | 写入/读取/覆盖逻辑，并发安全测试 |

**验收标准**：
- Agent 能采集 CPU/内存/磁盘/网络/系统信息并输出到 stdout（JSON）
- Agent 重启后流量计数从 state.json 恢复，月界按 UTC
- Server 能初始化 SQLite（WAL 生效）并通过 repository 层 CRUD
- 环形缓冲单元测试覆盖率 ≥ 90%
- 所有采集器不需要 root 权限

#### M2: Agent-Server 通信 + 注册上线（2 周）

**目标**：Agent 能注册到 Server 并维持 WebSocket 连接，数据能落地。

| 任务 | 类型 | 说明 |
|------|------|------|
| WebSocket 服务端 | TDD | WS 端点，连接管理，消息分发；**读限制 64KB + 写超时 + 可选压缩**（v1.3） |
| WebSocket 客户端 | TDD | Agent 端 WS 客户端，自动重连（指数退避 + **±20% jitter**，v1.3），心跳维持 |
| REST 注册流程 | TDD | 注册码生成/验证/一次性消费，Token 签发；**Token/注册码哈希存储（S9）**（v1.3）；**注册接口 IP 限速 5 次/分钟**（v1.3） |
| 主机指纹 | TDD | 指纹生成，注册时绑定，连接时校验 |
| 强制 TLS + Pinning | 代码 | Server TLS 配置；Agent 证书指纹校验（系统链 OR 指纹），拒绝明文 |
| 数据上报协议 | TDD | report/heartbeat 消息序列化/反序列化（帧内无 Token） |
| 配置拉取 | TDD | Agent 定时 GET 拉取探测目标配置 |
| 数据合理性校验 | TDD | 上报数据范围校验，频率限制 |
| 全局限速中间件 | TDD | 全局 API 限速 120 次/分钟/IP（v1.3） |
| 离线检测 | TDD | WS close 即时离线 + 心跳超时兜底（v1.3） |

**验收标准**：
- Agent 能通过 REST 注册接口用注册码换取 Token；服务端数据库中无 Token 明文
- Agent 能维持 WebSocket 长连接，3 秒上报一次
- 注册码一次性消费，15 分钟过期；指纹冲突返回 409；注册接口限速生效
- 全程 TLS 加密 + 证书 Pinning，明文连接与假证书均被拒绝
- 断线自动重连（1s→2s→4s→…→60s，含 jitter）；WS 断开即时离线
- 帧超 64KB 被断开并记录安全日志

#### M3: 实时监控 + 前端面板（3 周）

**目标**：浏览器面板以 NodeGet 风格实时展示服务器状态（延迟数据在 M4 接入前用占位/mock）。**v1.3：移除地图视图与 GeoIP（移 v2），工期 4 周→3 周。**

| 任务 | 类型 | 说明 |
|------|------|------|
| 前端项目初始化 | 代码 | React + Vite + shadcn/ui + Tailwind + ECharts + Zustand + React Router；M0 tokens 接入 |
| 设计系统基础组件 | TDD | 玻璃拟态卡片、RingProgress 圆环、LatencyGrid 延迟格子（mock 数据）；空/加载态 Skeleton/Empty（v1.3） |
| 登录页 | TDD | 密码登录，限速，JWT Cookie（2h + 静默续期） |
| WebSocket 实时推送 | TDD | Server→浏览器 WS 推送，前端 Zustand store，断线重连/页面可见性恢复 |
| 仪表盘卡片视图 | 代码 | 玻璃拟态卡片：国旗+标签徽章、三圆环指标、实时上下行速度、月流量进度条、到期/费用，离线卡片降透明度 |
| 仪表盘表格视图 | 代码 | 紧凑表格 + 迷你进度条 + 概览条（延迟列 M4 接入前显示占位） |
| 视图切换组件 | 代码 | 卡片/表格切换 + 预留"地图"入口位（置灰提示 v2，v1.3） |
| 筛选栏 | 代码 | 搜索/标签/地区/IPv4/IPv6/排序组合筛选，URL 参数持久化 |
| 服务器详情页 | 代码 | 延迟格子图置顶（mock）+ CPU/内存实时折线 + 磁盘/网络/月流量卡片 + 信息与费用卡片 |
| 时间范围切换 | 代码 | 1H/6H/12H/1D/2D/7D/30D（v1.3 七档）；1H/6H 实时，12H+ 轮询，7D/30D 读日聚合（数据 M4 接入） |
| 元数据编辑 + 标签管理 | TDD | 设置页：显示名称/位置/到期/费用/流量配额编辑，标签 CRUD（彩色徽章），删除服务器（二次确认+级联清理）（v1.3） |
| 会话管理页 | TDD | 会话列表/吊销/登出所有设备（v1.3 新增） |
| 主题系统 | 代码 | 浅色/深色/跟随系统 + 背景切换（纯色/渐变/自定义 https URL） |
| i18n 字典 | 代码 | 字符串集中管理（v1 仅中文，v1.3 新增） |
| Go embed 前端 | 代码 | 前端构建产物内嵌到 Server 二进制；静态资源缓存头+gzip（v1.3） |
| 面板 WS 认证 | TDD | JWT 认证 WS 端点 |

**验收标准**：
- 仪表盘卡片/表格双视图切换正常，地图入口位预留
- 卡片含圆环指标、月流量进度条、到期/费用展示，离线卡片降透明度
- 筛选组合生效且 URL 参数持久化
- 详情页 CPU/内存折线图实时刷新，LatencyGrid 组件单测通过（真实数据 M4 接入）
- 服务器元数据/标签/删除、会话管理均可用
- 主题与背景切换正常；双主题对比度达标；reduced-motion 生效
- 7 档时间范围切换正常，1H/6H 实时刷新
- 单二进制部署（前端内嵌）

#### M4: 网络探测(Ping) + 历史数据（2 周）

**目标**：Agent 执行三网 Ping 探测，历史数据落盘和查询。

| 任务 | 类型 | 说明 |
|------|------|------|
| ICMP Ping 采集器 | TDD | pro-bing privileged 模式，10 包采样，完整统计 |
| Unprivileged ICMP | TDD | 降级方案，Linux SOCK_DGRAM（依赖安装脚本写入 ping_group_range，v1.3） |
| TCP Ping 采集器 | TDD | 5 次采样，降级方案 |
| HTTP Ping 采集器 | TDD | 3 次采样，排除 DNS 时间 |
| Ping 方法自动选择 | TDD | privileged→unprivileged→TCP 降级逻辑 |
| 探测目标配置同步 | TDD | Agent 拉取目标列表，本地缓存；预置 5 个 is_default 种子目标 |
| 5 分钟聚合落盘 | TDD | 每 5 分钟聚合实时数据写入 metric_records |
| **日聚合落盘** | TDD | 每日 UTC 00:00 聚合前一日写入 metric_records_daily（v1.3 新增） |
| 历史数据查询 API | TDD | `?range=1h|6h|12h|1d|2d|7d|30d`（v1.3 含日聚合档） |
| 历史数据清理 | TDD | 定时清理超期数据（5 分钟粒度 90 天 / 日粒度 365 天） |
| 延迟格子数据接入 | 代码 | 卡片/表格/详情页延迟格子接入真实 Ping 历史 |
| 延迟丢包融合图 | 代码 | 详情页延迟折线 + 丢包柱状图共享 X 轴（按目标区分 v4/v6） |

**验收标准**：
- ICMP Ping 10 包采样，报告平均/最小/最大/抖动/丢包率
- Ping 方法自动降级正常（含 ping_group_range 未生效时落到 TCP）
- 历史数据 5 分钟 + 日聚合两级落盘，查询 API 正常
- 超期数据自动清理
- 仪表盘卡片/表格/详情页延迟格子展示真实探测数据，悬停显示该次完整统计
- 7D/30D 视图展示日聚合数据

#### M5: 告警 + 通知 + 安全加固（1.5 周）

**目标**：告警引擎、通知渠道、安全审计全部就绪。**v1.3：移除 TOTP（移 v2），工期 2 周→1.5 周。**

| 任务 | 类型 | 说明 |
|------|------|------|
| 告警规则 CRUD | TDD | API + 前端管理页（含 traffic_quota/expire_days 指标） |
| 告警状态机 | TDD | OK→PENDING→FIRING→RESOLVED，状态持久化到 alert_history，重启恢复 |
| 通知去重 + 静默期 | TDD | 同一告警静默期内不重复 |
| Webhook 通知 + SSRF 防护 | TDD | 内网过滤/重定向检测/响应体限制 |
| Telegram 通知 | TDD | Bot API 发送（固定 api.telegram.org） |
| 邮件通知 | TDD | SMTP 发送（独立 Dialer SSRF 检查） |
| 公开分享页 | 代码 | 与仪表盘同款双视图 UI，白名单字段，logo_url scheme 白名单，站点标题/Logo/页脚可配置 |
| 密码找回 CLI | 代码 | `probe-server reset-password`（v1.3 新增） |
| 安全审计 | 代码 | 按 7.8 清单逐项审计 |

**验收标准**：
- 告警规则能正确触发和恢复，含 traffic_quota/expire_days 两类指标
- Server 重启后告警状态从 alert_history 恢复，不重复发送
- Webhook/Telegram/SMTP 通知均经过 SSRF 防护，内网地址被拒绝
- 通知去重和静默期生效
- 公开分享页双视图正常且无敏感字段，logo_url 校验生效
- 安全审计清单全部通过

#### M6: 部署 + 发布 + 文档（1 周）

**目标**：可发布的生产版本。

| 任务 | 类型 | 说明 |
|------|------|------|
| Docker 镜像构建 | 代码 | 多阶段构建，多架构；安全加固（read_only/no-new-privileges/cap_drop ALL） |
| Server 一键安装脚本 | 代码 | 检测架构/下载/配置/systemd；首次启动生成 jwt_secret |
| Agent 一键安装脚本 | 代码 | 严格按 8.3：注册码/创建用户/install_salt/证书指纹/ping_group_range/setcap/systemd |
| Agent 升级脚本 | 代码 | 手动升级（不动配置与 state.json） |
| 备份脚本 | 代码 | sqlite3 .backup 方式（v1.3） |
| CI/CD 安全检查 | 代码 | noexec 零匹配 + gosec + gitleaks（v1.3 新增，M2 起随代码演进持续生效） |
| GitHub Release | 代码 | 自动构建发布（含 Agent 二进制内嵌的 Server 与独立 Agent 二进制） |
| 用户文档 | 文档 | 安装/配置/使用/FAQ（含证书指纹轮换、指纹重置、Token 重置、密码找回说明） |
| 安全文档 | 文档 | SECURITY.md，安全报告流程 |

**验收标准**：
- Docker 部署成功
- 一键安装脚本在 Ubuntu/Debian/CentOS 上测试通过（Agent 从 Server /download 端点成功安装上线）
- GitHub Release 包含 linux/amd64 与 linux/arm64 Server 二进制和 SHA256 校验
- CI 安全检查全部通过
- 文档覆盖安装、配置、使用

### 9.3 总工期与优先级

| 里程碑 | 工期 | 累计 | 优先级 |
|--------|------|------|--------|
| M0 设计系统与视觉基线 | 0.5 周 | 0.5 周 | P0 |
| M1 基础架构 + 核心采集 | 2 周 | 2.5 周 | P0 |
| M2 通信 + 注册上线 | 2 周 | 4.5 周 | P0 |
| M3 实时监控 + 前端面板 | 3 周 | 7.5 周 | P0 |
| M4 网络探测 + 历史数据 | 2 周 | 9.5 周 | P0 |
| M5 告警 + 通知 + 安全加固 | 1.5 周 | 11 周 | P1 |
| M6 部署 + 发布 + 文档 | 1 周 | 12 周 | P1 |

**总计约 12 周（AI 辅助并行开发可压缩至 10~11 周）**，M0-M4 是最小可用版本（MVP），M5-M6 是完整版本。

### 9.4 技术风险与缓解

| 风险 | 影响 | 缓解措施 |
|------|------|---------|
| pro-bing 库在特定平台行为不一致 | Ping 数据不准 | M4 阶段在多平台测试，记录差异 |
| 环形缓冲并发写入竞争 | 数据丢失或 panic | M1 阶段充分测试并发安全，用 sync.Mutex 或 channel |
| SQLite 高频写入性能 | Server 卡顿 | 环形缓冲吸收高频写入，SQLite 只存聚合数据 |
| **NVML dlopen 平台差异**（v1.3 新增） | GPU 采集失败或崩溃 | dlopen 失败/符号缺失时优雅降级"不可用"，不影响其他采集；不参与 Agent 核心路径 |
| **ping_group_range 未生效**（v1.3 新增） | unprivileged ICMP 失败 | 安装脚本写入 sysctl.d；运行时自检失败自动降级 TCP 并标注 |
| **WAL 备份不一致**（v1.3 新增） | 备份文件损坏 | 使用 sqlite3 .backup API，禁止直接 cp |
| WebSocket 断线重连风暴 | Server 连接数飙升 | 指数退避重连 + ±20% jitter（v1.3 补 jitter） |
| 前端 embed 后调试困难 | 开发效率低 | 开发模式用独立前端 dev server，构建时才 embed |
| Server 二进制膨胀（内嵌 Agent 二进制） | 下载/分发变慢 | 已决策接受（自包含价值大于体积）；Release 同时提供独立 Agent 二进制 |

---
## 10. AI 开发提示词

以下提示词可直接用于 AI 编程工具（如 TRAE、Cursor、Claude）进行开发。按里程碑顺序使用，每个里程碑完成后进入下一个。

### 10.1 通用上下文提示词（每次对话开始时使用）

```
你正在开发 XProbe——一款安全优先的服务器探针系统（v1 仅支持 Linux：Server 与 Agent 均只构建
linux/amd64 与 linux/arm64，Windows/macOS 留待 v2）。请遵循以下核心安全原则（不可妥协）：

1. 纯只读架构：Server 到 Agent 方向不存在任何控制通道，Agent 只采集和上报
2. 强制 TLS：所有通信强制加密，不允许关闭；Agent 通过证书指纹 Pinning 验证 Server 身份
3. 非 root 运行：Agent 始终以非特权用户运行
4. 无远程执行：代码中不引入 os/exec、pty、shell 等任何命令执行功能（CI noexec 检查零匹配）
5. 单管理员 + 强认证：不引入多租户；JWT 2h 有效期 + 静默续期 + sessions 表吊销
6. SSRF 防护：所有 Server 发起的对外请求（Webhook/Telegram/SMTP）做内网地址过滤
7. 最小权限采集：只采集不需要 root 的数据（GPU 走纯 Go dlopen NVML，禁止 exec）
8. 配置文件权限 600
9. 凭证哈希存储：Agent Token/注册码/会话在数据库仅存 SHA256 哈希，不存原文

技术栈：Go 1.22+ (Gin + gorilla/websocket + database/sql 原生 SQL 优先 + SQLite WAL)
+ React 18 + TypeScript + Vite + shadcn/ui + Tailwind(仅语义token) + ECharts + Zustand + pro-bing

v1 范围（v1.3 定案）：
- 仪表盘 = 卡片/表格双视图（地图视图与 GeoIP 在 v2，视图切换组件预留入口位）
- 无 TOTP（v2）；有会话管理（列出/吊销/登出全部设备）与 reset-password CLI
- 公开分享页、月流量配额/到期成本管理保留在 v1
- 详情页时间范围 7 档：1h/6h/12h/1d/2d/7d/30d（后两档读日聚合表）
- Agent 二进制由 Server 内嵌分发（/download/agent/:os/:arch）
- 月流量月界统一 UTC；CPU 首采样置空；WS 重连指数退避+±20% jitter；WS 读限制 64KB

通信方式：
- 注册/配置拉取/证书指纹获取：HTTPS REST（POST /api/v1/agent/register、GET /api/v1/agent/config、GET /api/v1/server-cert）
- 实时上报：WebSocket（强制 wss，可选 permessage-deflate），Token 通过 Authorization: Bearer 头携带
  （禁止放 URL 查询参数，避免反代日志泄露；帧内不携带 Token）

WebSocket 消息协议（严格按 5.2 节，仅 4 种帧）：
- Agent → Server: report, ping_result, heartbeat
- Server → Agent: heartbeat_ack（仅此 1 种）
- 不存在 register/config_update 等任何下发帧；注册、配置变更全部走 REST 拉取

其他硬性要求：
- 安全响应头中间件：HSTS/CSP/X-Frame-Options: DENY/nosniff/Referrer-Policy（7.7 节）
- 分享页 logo_url 仅允许 https:// scheme
- i18n 字符串集中管理；Tailwind 禁止 raw 色值类，一律语义 token

详细设计文档见：docs/server_probe_design_v1.3.md
```

### 10.2 M0 提示词：设计系统与视觉基线（v1.3 新增）

```
请完成 XProbe 的 M0 里程碑：设计系统与视觉基线。本阶段不写业务组件。

任务清单：
1. 生成设计系统基准：
   - 用 ui-ux-pro-max 检索 "server monitoring dashboard dark glassmorphism" --design-system --persist，
     产出持久化到 docs/design-system/MASTER.md（风格/配色/字体/反模式/交付前清单）
2. 差异化定调（依 frontend-design 原则）：
   - 确定字体气质与背景纹理层次；规避紫渐变+白底、千篇一律的通用字体
   - 深色主题玻璃拟态为主推；浅色主题用中性冷灰/品牌色极浅渐变
3. 密度校准（依 frontend-skill"App UI 克制"章节）：
   - 信息密度高而可读、accent 色克制、工具性文案基调（状态导向标题）
4. 落地为 tokens：
   - shadcn 语义 token 双主题变量表（--background/--foreground/--primary/--card/--border/--muted 等）
   - 延迟色阶 --lat-1 ~ --lat-6 双主题各一套；状态色（成功/警告/危险）token 化
   - 字体栈（系统栈 + tabular-nums）、间距/圆角/阴影/动效时长（150-300ms）规范
5. 可访问性基线：双主题全部前景/背景组合对比度 ≥4.5:1 校验清单；focus-visible；44px 触控；
   375/768/1024/1440 断点规范

产出物：
- docs/design-system/MASTER.md（设计系统文档）
- Tailwind CSS 变量与 shadcn 主题配置草案（仅配置文件，不含业务组件）

验收标准：
- 双主题（尤其浅色）延迟色阶与状态色对比度校验通过
- 核心组件视觉规格确认：GlassCard / RingProgress / LatencyGrid / MiniBar
```

### 10.3 M1 提示词：基础架构 + 核心采集

```
请实现 M1 里程碑：基础架构 + 核心采集。

按照 TDD 方式开发，先写测试再实现。

任务清单：
1. 初始化 Go module，创建目录结构（server/ 和 agent/ 两个子项目，共享 model 包）
2. 实现 Agent 采集器（internal/collector/）：
   - cpu.go: 读 /proc/stat 计算 CPU 使用率（两次采样差值；Agent 启动后首个采样周期值置为 null，
     避免单采样假值），读 /proc/cpuinfo 获取型号和核心数，读 /proc/loadavg 获取负载
   - memory.go: 读 /proc/meminfo 解析 MemTotal/MemAvailable/SwapTotal/SwapFree
   - disk.go: 系统调用 statfs 遍历挂载点获取磁盘使用率
   - network.go: 读 /proc/net/dev 计算网卡速率差值，读 /proc/net/tcp 和 /proc/net/udp 统计连接数
   - system.go: uname 系统调用获取系统信息，读 /proc/uptime 获取运行时间，遍历 /proc 统计进程数
3. 实现 Server 数据层（internal/repository/）：
   - sqlite.go: SQLite 连接管理（WAL 模式 + busy_timeout，建表用原生 SQL，GORM 仅做行映射）
   - ringbuffer.go: 内存环形缓冲（3 秒/点 × 3 小时 = 3600 点），支持并发安全写入读取，每 Agent 一个实例
4. 实现 Server 数据模型（internal/model/）：
   - agent.go, metric.go(含日聚合), alert.go, alert_history.go, notify.go, session.go
5. Agent 流量计数状态持久化（internal/state/state.go）：
   - 读写 /var/lib/probe-agent/state.json（上次累计字节数与时间戳），Agent 重启后据此续算速率，
     避免流量统计跳变；跨自然月归零，月界统一按 UTC 判定

验收标准：
- Agent 采集器能采集所有指标并输出 JSON 到 stdout；CPU 首采样为 null
- Agent 重启后流量计数从 state.json 恢复，速率无跳变；月界 UTC
- Server 能初始化 SQLite（WAL 生效）并通过 repository 层 CRUD
- 环形缓冲单元测试覆盖率 ≥ 90%
- 所有采集器不需要 root 权限

请参考设计文档第 4.1 节（采集项清单）、第 4.3 节（state_file 配置）和第 5.6 节（SQLite 表结构）。
```

### 10.4 M2 提示词：通信 + 注册上线

```
请实现 M2 里程碑：Agent-Server 通信 + 注册上线。

按照 TDD 方式开发。

任务清单：
1. Server 端 WebSocket 端点（/api/v1/agent/report）：
   - 连接管理（Agent 连接池 map[agentID]*Conn）
   - 消息分发（仅 report / ping_result / heartbeat 三种入站帧，回复 heartbeat_ack）
   - 连接建立时校验 Authorization: Bearer Token（数据库比对 SHA256 哈希）与主机指纹
   - 离线检测双路径：WS close 事件即时置离线；90 秒心跳超时兜底
   - 传输限制：单帧读限制 64KB（超出断开+安全日志）、写超时 10 秒、可选 permessage-deflate 压缩
2. Agent 端 WebSocket 客户端：
   - 连接维持，自动重连（指数退避 1s→60s，加 ±20% 随机 jitter）
   - 心跳发送（每 30 秒），Server 回 heartbeat_ack
   - 数据上报（每 3 秒打包采集数据发送；帧内不携带 Token）
3. REST 注册流程（不走 WebSocket，严格按 4.2 节 6 步）：
   - Server: POST /api/v1/agent/register，注册码随机、一次性消费、15 分钟过期（401/409 区分错误）
   - Server: 注册接口 IP 限速 5 次/分钟/IP
   - Server: 注册码仅存 SHA256 哈希（S9），校验时哈希比对
   - Server: Token 随机 32 字节、持久化（仅存哈希）、绑定主机指纹；
     指纹 = SHA256(install_salt + CPU型号 + 主网卡MAC + GOOS)，不含主机名（按 7.5 节）
   - Server: 指纹冲突返回 409，提示管理员调用 reset-fingerprint 接口
   - Agent: 首次启动用注册码注册获取 Token，保存到配置文件（权限 600）
4. TLS 证书 Pinning（重点安全）：
   - Server: TLS 配置，无证书时自动生成自签证书，并通过 GET /api/v1/server-cert 暴露指纹
   - Agent: 安装时带外获取证书指纹写入 server_cert_fingerprints 数组（支持 [旧, 新] 双指纹轮换）
   - Agent: TLS 校验 = 系统证书链 OR 指纹匹配，拒绝 http:// 和 ws://；tls_insecure 仅供调试且界面显示黄色徽章
5. 配置拉取：
   - Agent: 每小时 HTTPS GET /api/v1/agent/config 拉取探测目标配置（Authorization 头携带 Token）
   - 拉取失败使用本地缓存
6. 中间件：
   - 数据合理性校验：上报数据范围（CPU 0-100%、内存 used≤total 等）、频率限制（过快拒绝/过慢离线）、
     单帧 >64KB 断开
   - 全局 API 限速中间件（120 次/分钟/IP）+ 登录限速（5 次/分钟/IP，连续 10 次失败锁 15 分钟）
   - 安全响应头中间件（HSTS/CSP/X-Frame-Options/nosniff/Referrer-Policy，按 7.7 节）

WebSocket 消息协议严格按设计文档第 5.2 节实现（Server→Agent 仅 heartbeat_ack）。
安全要求严格按设计文档第 2 节和第 7 节实现。

验收标准：
- Agent 能通过 REST 注册接口用注册码换取 Token；数据库中无 Token/注册码明文（仅 SHA256 哈希）
- Agent 能维持 WebSocket 长连接，3 秒上报一次
- 注册码一次性消费，15 分钟过期；指纹冲突返回 409；注册接口限速生效
- 全程 TLS 加密 + 证书指纹 Pinning，明文连接与假证书均被拒绝
- 断线自动重连（1s→2s→4s→…→60s + jitter）；WS 断开即时离线
- 帧超 64KB 断开并记录安全日志；安全响应头全部下发
```

### 10.5 M3 提示词：实时监控 + 前端面板

```
请实现 M3 里程碑：实时监控 + 前端面板。

任务清单（延迟真实数据在 M4 接入，本阶段用 mock）：
1. 前端项目初始化：
   - React 18 + TypeScript + Vite + shadcn/ui + Tailwind CSS + ECharts + Zustand + React Router
   - 接入 M0 产出的设计 tokens；Tailwind 仅用语义 token，禁止 raw 色值类
2. 设计系统基础组件（TDD：Vitest + Testing Library，mock 数据驱动测试）：
   - 玻璃拟态卡片（半透明背景 + backdrop-blur + 细边框）
   - RingProgress 圆环指标组件（CPU/内存/磁盘三圆环；CPU 首采样 null 显示 --）
   - LatencyGrid 延迟格子组件（色块网格用 --lat-* 变量，悬停显示该次完整统计；M3 用 mock 数据；
     尊重 prefers-reduced-motion）
   - 空状态/加载态：Skeleton/Empty 组件覆盖无服务器/无告警/图表加载等场景
3. 登录页：
   - 密码登录，限速 5 次/分钟，连续失败 10 次锁定 15 分钟
   - JWT 存储在 HttpOnly Cookie（SameSite=Strict, Secure=true），2 小时过期 + 剩余 30 分钟内静默续期
   - （无 TOTP，v2 提供）
4. WebSocket 实时推送：
   - Server 端 /ws/dashboard 端点，JWT 认证（Cookie）
   - 每 3 秒推送所有在线 Agent 的实时数据
   - 前端 Zustand store 管理实时数据状态，断线重连 + 页面可见性恢复
5. 仪表盘卡片视图（NodeGet 风格玻璃拟态卡片）：
   - 国旗 + 标签彩色徽章行
   - CPU/内存/磁盘三 RingProgress 圆环指标
   - 实时上下行速度、月流量进度条（已用/配额，超 80% 橙、100% 红）
   - 到期日期/月费用展示
   - 离线卡片整体降透明度处理
   - 点击卡片跳转详情页
6. 仪表盘表格视图：
   - 紧凑表格 + 迷你进度条 + 概览条
   - 延迟列动态生成（按 ping_targets 实际目标，含 v6 徽章，无 v6 目标显示"无v6"，M3 占位）
7. 视图切换组件：卡片/表格切换，预留"地图"入口位（置灰 + tooltip"v2 提供"）
8. 筛选栏：
   - 搜索/标签/地区/IPv4/IPv6/排序组合筛选
   - 筛选状态 URL 参数持久化（刷新/分享后保持）
9. 服务器详情页：
   - LatencyGrid 延迟格子图置顶（mock 数据），IPv4/IPv6 分组
   - CPU/内存实时折线图（ECharts）
   - 磁盘/网络/月流量卡片 + 信息与费用卡片
   - 时间范围切换：1H/6H/12H/1D/2D/7D/30D（7 档）
   - 1H/6H 用环形缓冲实时数据（3 秒/点），WebSocket 推送刷新
   - 12H/1D/2D 用 5 分钟聚合数据，定时轮询（5 分钟）
   - 7D/30D 用日聚合数据（metric_records_daily），定时轮询（30 分钟）——数据在 M4 接入，先实现 UI 与 API 契约
   - 6H 需混合数据源拼接（最近 3 小时实时 + 3-6 小时聚合，按 5.3 节聚合规则）
10. 元数据编辑 + 标签管理（TDD，设置页）：
   - 服务器显示名称/位置/到期日期/月费用/流量配额编辑
   - 标签 CRUD（彩色徽章），支持删除时二次确认
   - 删除服务器：二次确认，提示级联清理范围
11. 会话管理页（TDD，设置页）：
   - 会话列表（设备/IP/最近活跃）、吊销单个会话、"登出所有设备"
12. 主题系统：
   - 浅色/深色/跟随系统，localStorage 存储，CSS 变量切换
   - 背景切换：纯色/渐变/自定义图片 URL（仅 https）
13. i18n 字符串集中管理（v1 仅中文）
14. Go embed 前端：
   - web/ 为唯一 embed 源，前端构建产物内嵌 Server 二进制；静态资源 immutable 缓存头 + gzip

验收标准：
- 管理员能登录面板（含锁定与静默续期）；会话管理可用
- 仪表盘卡片/表格双视图切换正常，地图入口位预留
- 卡片含圆环指标、月流量进度条、到期/费用展示，离线卡片降透明度
- 筛选组合生效且 URL 参数持久化
- 详情页 CPU/内存折线图实时刷新，LatencyGrid 组件单测通过（真实数据 M4 接入）
- 服务器元数据/标签/删除、会话管理均可用
- 主题与背景切换正常；双主题对比度达标；reduced-motion 生效
- 7 档时间范围切换正常，1H/6H 实时刷新
- 单二进制部署（前端内嵌）
```

### 10.6 M4 提示词：网络探测 + 历史数据

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
   - 运行时自检 ping_group_range 未生效时自动落到 TCP Ping 并标注实际方式
3. TCP Ping 采集器：
   - 5 次采样，每次超时 5 秒，间隔 0.5 秒；DNS 预解析；报告 5 次平均延迟和失败比例
4. HTTP Ping 采集器：
   - 3 次采样，每次超时 10 秒；自定义 DialContext 排除 DNS 时间；状态码 2xx-3xx 为成功
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
   - 每 5 分钟将环形缓冲实时数据聚合为一个点写入 metric_records
   - 聚合规则严格按 5.3 节：CPU/内存/负载取平均，磁盘按挂载点分别平均，网络取平均速率，
     Ping 取 avg/min/max/loss 均值
   - 每日 UTC 00:00 将前一日聚合为 metric_records_daily 一行（CPU/内存 avg+max，网络平均+峰值，
     Ping 日均值+最差丢包）
8. 历史数据查询 API：
   - GET /api/v1/servers/:id/history?range=1h|6h|12h|1d|2d|7d|30d
   - 1h/6h 从环形缓冲读取，12h/1d/2d 从 metric_records 读取，7d/30d 从 metric_records_daily 读取
9. 历史数据清理：
   - 定时清理超期数据（5 分钟粒度 90 天 / 日粒度 365 天）
10. 延迟格子数据接入：
   - M3 的 LatencyGrid 组件接入真实 Ping 历史（环形缓冲最近 60 分钟）
   - 卡片/表格/详情页三处统一接入，悬停显示该次完整统计
11. 延迟丢包融合图：
   - 详情页延迟折线（每个目标一条线，区分 v4/v6）+ 丢包率柱状图共享 X 轴

Ping 方案必须超越 Nezha 的准确性，参考设计文档第 4.6 节。

验收标准：
- ICMP Ping 10 包采样，报告完整统计（平均/最小/最大/抖动/丢包率）
- Ping 方法自动降级正常（含 ping_group_range 未生效场景）
- 历史数据 5 分钟 + 日聚合两级落盘，查询 API 正常
- 仪表盘卡片/表格/详情页延迟格子展示真实探测数据，悬停显示完整统计
- 融合图按目标 ip_version 区分 v4/v6 线条
- 7D/30D 视图展示日聚合数据；超期数据自动清理
```

### 10.7 M5 提示词：告警 + 通知 + 安全加固

```
请实现 M5 里程碑：告警 + 通知 + 安全加固。

按照 TDD 方式开发。（TOTP 不在本里程碑范围，v2 提供）

任务清单：
1. 告警规则 CRUD（API + 前端管理页）：
   - 规则字段：name, metric(cpu_usage/mem_usage/disk_usage/agent_offline/traffic_quota/expire_days),
     operator(>/</=), threshold, duration, enabled, notify_channel_id
   - metric 语义严格按 5.4 节：traffic_quota 传月流量已用百分比（operator 通常 >），
     expire_days 传剩余天数（operator 通常 <）
2. 告警状态机（状态持久化）：
   - 状态：OK → PENDING(超阈值但未达duration) → FIRING(达到duration) → RESOLVED(恢复正常)
   - 状态写入 alert_history 表（rule_id/agent_id/status/value/started_at/updated_at/notified），
     Server 重启后恢复现场，不重复通知
   - 进入 FIRING 时发送告警通知，进入 RESOLVED 时发送恢复通知
3. 通知去重 + 静默期：
   - 同一告警 FIRING 状态下，静默期内（默认 60 分钟）不重复发送
4. Webhook 通知 + SSRF 防护（重点安全）：
   - 内网地址过滤：10/8, 172.16/12, 192.168/16, 127/8, 169.254/16, ::1, fc00::/7
   - DNS 重绑定防护：自定义 Dialer 强制使用预解析 IP
   - 重定向检测：CheckRedirect 中再次 SSRF 检查
   - 响应体限制：最多读 1KB，不反射给用户
   - 超时 10 秒，强制 TLS 验证
5. Telegram 通知：Bot API 发送（固定 api.telegram.org，同套 SSRF 防护）
6. 邮件通知：SMTP 发送，SMTP 主机名走独立 Dialer SSRF 检查（按 7.4 节）
7. 公开分享页：
   - /s/:share_id（UUIDv4），免登录
   - 与仪表盘同款卡片/表格双视图 UI（无地图），白名单字段仅展示非敏感信息（无 IP/Token/配置）
   - 站点标题/Logo/页脚可配置；logo_url 仅允许 https:// scheme；标题/页脚 React 转义渲染
8. 密码找回 CLI：
   - probe-server reset-password --username admin（交互输入新密码，重置后吊销全部会话）
9. 安全审计：按设计文档第 7.8 节清单逐项审计，含 7.2 节可执行文件审计规则（Agent 零匹配 os/exec 等）

SSRF 防护实现参考设计文档第 7.4 节。

验收标准：
- 告警规则能正确触发和恢复，含 traffic_quota/expire_days 两类指标
- Server 重启后告警状态从 alert_history 恢复，不重复发送
- Webhook/Telegram/SMTP 通知均经过 SSRF 防护，内网地址被拒绝
- 通知去重和静默期生效
- 公开分享页双视图正常且无敏感字段，logo_url 校验生效
- reset-password CLI 可用且重置后旧会话全部失效
- 安全审计清单全部通过
```

### 10.8 M6 提示词：部署 + 发布 + 文档

```
请实现 M6 里程碑：部署 + 发布 + 文档。

任务清单：
1. Docker 镜像构建：
   - 多阶段构建（Node 构建前端 → Go 构建后端 → Alpine 运行时）
   - 多架构（linux/amd64, linux/arm64）
   - 安全加固：read_only, no-new-privileges, cap_drop ALL
2. Server 一键安装脚本：
   - 检测架构，下载二进制（校验 SHA256），创建配置，安装 systemd service
   - 首次启动自动生成 jwt_secret 并写回配置（按 8.2 节）；支持 PROBE_JWT_SECRET 环境变量
3. Agent 一键安装脚本（严格按 8.3 节）：
   - 解析参数（--server, --code, --cert-fingerprint）
   - 检测系统和架构（仅支持 Linux）
   - 从 Server 内嵌分发端点 /download/agent/linux/:arch 下载二进制（校验 SHA256）
   - 创建 probe 用户
   - 生成 install_salt（openssl rand -hex 32）
   - 带外获取 Server 证书指纹写入 server_cert_fingerprints
   - 写入 /etc/sysctl.d/99-probe-agent.conf（net.ipv4.ping_group_range = 0 2147483647）并 sysctl --system
   - 创建配置文件（权限 600）与状态目录 /var/lib/probe-agent（chown -R probe:probe）
   - 尝试 setcap cap_net_raw+ep
   - 安装 systemd service（User=probe, ReadWritePaths 含状态目录, 安全加固）
   - 启动服务
4. Agent 升级脚本（手动升级）：
   - 停止服务，备份旧版，替换新版，重新 setcap，启动服务
   - 升级不动配置与 state.json：install_salt 不变则主机指纹不变，避免误触发 409
5. 备份脚本：
   - sqlite3 /var/lib/probe-server/probe.db ".backup '/backup/xprobe-$(date +%Y%m%d).db'"
   - （WAL 模式下禁止直接 cp，按 8.6 节）
6. CI/CD：
   - 自动构建 Linux 二进制（Server amd64/arm64 内嵌 Agent；Agent amd64/arm64/armv7）+ SHA256 校验
   - 自动构建多架构 Docker 镜像
   - 安全检查 job：noexec 零匹配（Agent 源码/二进制 os/exec 检查）+ gosec + gitleaks，任一失败终止发布
7. 用户文档：安装/配置/使用/FAQ（含证书指纹轮换、指纹重置、Token 重置、密码找回、流量基线重置说明）
8. 安全文档：SECURITY.md，安全报告流程

注意：第一版不做 Agent 自动升级（违反纯只读架构）；v1 仅发布 Linux 二进制。

验收标准：
- Docker 部署成功
- 一键安装脚本在 Ubuntu/Debian/CentOS 上测试通过（Agent 从 Server /download 端点成功上线）
- GitHub Release 包含 linux/amd64 与 linux/arm64 Server 二进制和 SHA256 校验
- CI 安全检查（noexec/gosec/gitleaks）全部通过
- 文档覆盖安装、配置、使用
```

---
## 11. v2 路线图

**v1.3 新增章节**：以下功能在 v1.3 规划中移出 v1，按优先级排列。

| 功能 | 说明 | 前置条件 |
|------|------|---------|
| **地图视图 + GeoIP** | ECharts 世界地图 + effectScatter 光点；GeoLite2-City.mmdb 离线解析（管理员手动下载，MaxMind 免费注册），坐标缓存 `geo_lat`/`geo_lon` 字段；手动 region/country_code 优先于自动解析；无 mmdb 时优雅降级提示 | 无（设计已在原 5.7 章完成，原样保留） |
| **TOTP 两步验证** | 管理员可选开启；含恢复码与 CLI 重置流程（防锁死）；admin 表迁移添加 totp 字段 | 无 |
| **Windows/macOS Agent** | 改用 gopsutil 跨平台采集，功能子集仅含基本信息 + TCP Ping；发布矩阵扩展 | Agent 采集层抽象化 |
| **分享页在线率视图** | 状态页增加月度/90 天在线率百分比条与故障时间线（SLA 视图） | alert_history/心跳数据积累 |
| **更多通知渠道** | Discord、Slack、飞书、钉钉机器人；均走同一套 SSRF 防护实现 | 无 |
| **Prometheus /metrics 导出** | 只读指标暴露，供已有 Prometheus/Grafana 用户集成；默认关闭 | 无 |
| **i18n 英文** | v1 字符串已集中管理，补全 en 字典与语言切换 | 无 |
| **月度报告邮件** | 定期汇总（流量/可用性/成本）通过已有 SMTP 通道发送 | SMTP 通道 |
| **外观扩展** | OLED 纯黑主题、更多强调色预设、卡片密度选项 | M0 token 体系 |

**明确不采纳项**（安全红线，任何版本不引入）：
- 多租户/细粒度用户权限（违反 S5）
- Server→Agent 任何形式的控制通道（违反 S1），包括基于此能力的插件系统、WebSSH、文件管理器、Cron 分发
- Agent 自动更新（违反 S1+S4；保持手动升级 + 面板版本提示）
- Agent 侧任何命令执行能力（违反 S4）

---

## 12. 附录：Nezha 漏洞参考

以下漏洞信息用于理解本探针安全设计的背景和必要性。

| 漏洞 | 严重程度 | 描述 | XProbe 的防御 |
|------|---------|------|-----------|
| CVE-2026-46716 | Critical (9.9) | 跨租户 Cron RCE，RoleMember 可在所有服务器执行任意命令 | S1+S4：无控制通道，无命令执行 |
| GHSA-q6xx-5vr8-p898 | Critical (9.9) | 终端/文件管理器会话劫持，跨租户 RCE | S1+S4：无终端/文件管理器 |
| CVE-2026-46717 | High (7.7) | 通知 Webhook SSRF + 完整响应体反射 | S6：SSRF 防护层 |
| CVE-2026-47120 | High (7.1) | 告警规则触发他人 Cron 任务 | S1+S4：无 Cron 任务 |
| CVE-2026-48119 | High (7.1) | Agent 伪造他人服务监控结果 | S5：单管理员无跨租户问题 + 数据校验 |
| CVE-2026-49396 | Moderate | CSRF 触发存储的 Cron 命令 | S5：SameSite=Strict + 无 Cron |
| CVE-2026-47124 | Moderate (6.5) | WebSocket 跨租户遥测泄露 | S5：单管理员 + WS 认证 |
| NEZHA-AGENT-001 | High (7.5) | Agent 明文 gRPC 通道，凭证可被截获 | S2：强制 TLS + 证书 Pinning |

---

**文档结束**

本文档包含了 XProbe 服务器探针的完整设计规格和开发提示词。使用时：
1. 先通读全文理解整体设计
2. 按 M0→M6 顺序使用 AI 开发提示词（M0 设计系统是所有前端工作的前置）
3. 每个里程碑完成后对照验收标准检查
4. 开发完成后按第 7.8 节安全审计清单审计（CI 自动项 + 人工项）
