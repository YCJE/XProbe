# XProbe

安全优先的纯只读服务器探针(监控)系统。从架构上杜绝 RCE 攻击面(Server→Agent 无任何控制通道),功能对标 Nezha/Komari,视觉对标 NodeGet。

## 核心特性

- **纯只读架构**:WebSocket 协议中 Server→Agent 方向仅有 1 种心跳确认帧,不存在任何命令/配置下发帧(S1/S4,CI `audit-noexec` 自动验证 Agent 二进制零 `os/exec`)
- **强制 TLS + 证书 Pinning**:Agent 拒绝明文连接,支持自签/私有 CA 场景的指纹校验与双指纹平滑轮换
- **非 root 运行**:systemd 加固 + setcap 最小能力,ICMP 不可用时自动降级 unprivileged ICMP → TCP Ping
- **凭证哈希存储**:Agent Token / 注册码 / 登录会话在数据库仅存 SHA256 哈希
- **SSRF 防护**:Webhook/Telegram/SMTP 通知统一走内网过滤 + DNS 预解析 + 重定向检测 + 响应体限读
- **网络探测**:ICMP 10 包采样,报告延迟/最小/最大/抖动/丢包完整统计;IPv4/IPv6 双栈
- **NodeGet 风格仪表盘**:玻璃拟态卡片、SVG 圆环指标、延迟小格子图、卡片/表格/地图三视图(轻量地图:国家质心 + 手动经纬度,免 MaxMind)、深浅双主题(WCAG AA 对比度实测)、布局自定义(密度/显示项)
- **服务监控**:Server 主动对 HTTP/TCP/ICMP 端点拨测,45 天在线率日格 + 最近 64 次结果条 + DOWN/UP 转移通知,公开页集成(与 Agent 通道无关,不违反 S1)
- **报表**:近 12 个月流量堆叠图、月成本按币种合计与手动汇率折算 CNY、30 天续费提醒
- **告警 + 通知**:阈值状态机(防抖/静默/恢复通知),含月流量配额与 VPS 到期提醒;Webhook/Telegram/SMTP
- **公开分享页**:`/s/:share_id` 免登录状态页,白名单字段(无 IP/Token/配置)
- **单二进制部署**:前端 embed + Agent 二进制内嵌分发,一键安装完全自包含

## 安全原则

S1 纯只读 · S2 强制 TLS · S3 非 root · S4 无远程执行 · S5 单管理员强认证 · S6 SSRF 防护 · S7 最小权限采集 · S8 配置权限 600 · S9 凭证哈希存储

## 技术栈

Go 1.26 · Gin · gorilla/websocket · SQLite (WAL, 纯 Go 驱动) · pro-bing · React 18 + TypeScript · Vite · Tailwind CSS v4 · Zustand · ECharts

## 快速开始

### 1. 启动 Server

```bash
# 方式 A:Docker(推荐)
PROBE_JWT_SECRET=$(openssl rand -hex 32) docker compose up -d

# 方式 B:一键脚本(GitHub Release)
curl -fsSL https://raw.githubusercontent.com/YCJE/XProbe/main/scripts/install-server.sh | bash
```

### 2. 配置域名与 HTTPS 证书(推荐)

默认生成自签证书(浏览器有告警, Agent 用指纹 Pinning 不受影响)。有域名时建议换成正式证书。

#### 前置条件

1. 域名的 A 记录已解析到本服务器(`dig +short probe.example.com` 应返回你的 IP)
2. 云安全组/防火墙已放行 **80/tcp**(Let's Encrypt 验证走 80;XProbe 只用 443, 互不冲突)

#### 安装 certbot(系统默认没有, 需先装)

```bash
# Ubuntu / Debian
apt update && apt install -y certbot

# CentOS / AlmaLinux / Rocky
dnf install -y epel-release && dnf install -y certbot

# 或官方推荐的 snap 方式(版本最新)
snap install core && snap refresh core && snap install --classic certbot

certbot --version   # 验证安装
```

#### 签发证书

```bash
# standalone 模式会临时占用 80 端口完成验证(不影响运行中的 443):
certbot certonly --standalone -d probe.example.com

# 把证书放到服务可读位置并授权:
mkdir -p /var/lib/xprobe-server/certs
cp /etc/letsencrypt/live/probe.example.com/fullchain.pem /var/lib/xprobe-server/certs/
cp /etc/letsencrypt/live/probe.example.com/privkey.pem  /var/lib/xprobe-server/certs/
chown -R xprobe:xprobe /var/lib/xprobe-server/certs

# 写入配置(或全新安装时直接加参数: install-server.sh --cert <fullchain> --key <privkey> --domain probe.example.com):
# 编辑 /etc/xprobe-server/config.yml 的 tls.cert / tls.key 指向上方两个文件
systemctl restart xprobe-server
```

- **续期**:apt 方式安装会自动注册 `certbot renew` 定时器;证书续期后 XProbe 热加载自动生效, 无需重启
- **80 端口不方便开放?**:改用 DNS 验证(`certbot certonly --manual --preferred-challenges dns -d 域名`, 按提示加 TXT 记录), 或安装对应 DNS 插件实现全自动
- **没有域名?**:继续用自签证书即可, Agent 安装命令带上 `--cert-fingerprint <指纹>`(安装摘要会打印)

要点:
- **证书热加载**:续期(certbot renew)后自动生效,无需重启服务
- **指纹实时**:更换证书后 `curl -s https://probe.example.com/api/v1/server-cert` 返回新指纹,Agent 安装命令自动带上新指纹
- **已装 Agent 的轮换**:换证书后旧 Agent 会因指纹不匹配拒绝连接——把新旧两个指纹都写进 Agent 的 `server_cert_fingerprints` 列表(支持双指纹平滑轮换),或重装 Agent
- **反向代理场景**:nginx 终止 TLS 时,把 `config.yml` 的 `listen` 改为 `127.0.0.1:8443`,nginx `proxy_pass https://127.0.0.1:8443`(需 `proxy_set_header` 与 WebSocket 升级头);Server 端自签证书即可

### 3. 初始化与添加服务器

浏览器打开 `https://<域名或IP>/`,创建管理员账号(无默认密码);在「设置 → 注册码」生成一次性注册码,复制一键安装命令。

### 4. 被控服务器安装 Agent

```bash
curl -fsSL https://raw.githubusercontent.com/YCJE/XProbe/main/scripts/install-agent.sh | bash -s -- --server https://your-server.com --code ABC123XY
```

安装脚本自动:校验 SHA256 → 创建非特权用户 → 生成 install_salt → 获取证书指纹(Pinning)→ 解锁 unprivileged ICMP → setcap → systemd 服务。
> 也可在被控服务器放一份 `scripts/install-agent.sh` 后从 `https://your-server.com/install.sh` 提供(脚本与二进制同源校验)。

> 注:源码 checkout 直接编译的 Server 不含前端面板(访问显示占位页),官方 Release/Docker 产物已内嵌;本地开发请先 `make build-frontend`。

### 5. 升级 / 卸载

```bash
# Server 升级(自动备份数据库; 旧二进制保留 .bak 可回滚)
curl -fsSL https://raw.githubusercontent.com/YCJE/XProbe/main/scripts/upgrade-server.sh | bash

# Agent 升级(配置/install_salt 不动, 主机指纹不变)
curl -fsSL https://raw.githubusercontent.com/YCJE/XProbe/main/scripts/upgrade-agent.sh | bash -s -- --server https://your-server.com

# 卸载(默认保留数据, 重装自动接回; 加 --purge 彻底清除)
curl -fsSL https://raw.githubusercontent.com/YCJE/XProbe/main/scripts/uninstall-server.sh | bash
curl -fsSL https://raw.githubusercontent.com/YCJE/XProbe/main/scripts/uninstall-agent.sh | bash
curl -fsSL https://raw.githubusercontent.com/YCJE/XProbe/main/scripts/uninstall-agent.sh | bash -s -- --purge
```

忘记管理员密码:`xprobe-server reset-password --username admin`(重置后吊销全部会话)。

### 6. 数据备份

```bash
scripts/backup.sh /var/lib/xprobe-server   # sqlite3 .backup 在线一致性快照(WAL 下禁止直接 cp)
```

## 文档

- [设计文档 v1.3](docs/server_probe_design_v1.3.md) —— 产品规格、架构、安全设计、部署运维、里程碑(M0-M6)与 AI 开发提示词
- [设计系统 MASTER](docs/design-system/MASTER.md) —— 前端视觉唯一依据(双主题 tokens、WCAG 对比度实测、组件规格)
- [改进路线图](docs/ROADMAP.md) —— 与 Nezha/Komari/NodeGet 的功能对标与实施顺序
- [Agent 占用基准](docs/BENCHMARK.md) —— 测量方法、当前实测与优化方向

## 开发

```bash
make test             # Go 全量测试(12 包, 60+ 用例)
make build-frontend   # 构建面板并拷贝到 server/web(编译带面板的 Server 前必跑)
make build-linux      # 快速交叉编译(不含面板;发布请用 make release)
make release          # 发布产物:前端内嵌 + Agent 内嵌 + SHA256(与 CI 同口径)
make audit-noexec     # S4 审计门禁:Agent 代码零命令执行符号
cd frontend && npm test   # 前端逻辑测试
```

## 开发进度

- [x] M0 设计系统与视觉基线
- [x] M1 基础架构 + 核心采集
- [x] M2 Agent-Server 通信 + 注册上线
- [x] M3 实时监控 + 前端面板
- [x] M4 网络探测(Ping)+ 历史数据
- [x] M5 告警 + 通知 + 安全加固
- [x] M6 部署 + 发布 + 文档

## License

待定(TBD)
