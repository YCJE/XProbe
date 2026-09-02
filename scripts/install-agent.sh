#!/usr/bin/env bash
# ============================================================
#  XProbe Agent 一键安装脚本(在被控服务器上执行)
#  用法:
#    curl -fsSL https://raw.githubusercontent.com/YCJE/XProbe/main/scripts/install-agent.sh \
#      | bash -s -- --server https://your-server.com --code ABC123XY
#  可选: --cert-fingerprint <sha256>  自签证书场景带外传递指纹
# ============================================================
set -euo pipefail

SERVER_URL=""
REGISTER_CODE=""
CERT_FP=""
while [[ $# -gt 0 ]]; do
    case $1 in
        --server) SERVER_URL="$2"; shift 2;;
        --code) REGISTER_CODE="$2"; shift 2;;
        --cert-fingerprint) CERT_FP="$2"; shift 2;;
        *) echo "未知参数: $1"; exit 1;;
    esac
done
[[ -z "$SERVER_URL" || -z "$REGISTER_CODE" ]] && { echo "用法: 需要 --server 与 --code"; exit 1; }
case "$SERVER_URL" in
    https://*) ;;
    *) echo "S2: --server 必须 https:// (明文下载会被 MITM 替换)"; exit 1;;
esac

if [ -t 2 ]; then
    C_OK=$'\033[32m'; C_WARN=$'\033[33m'; C_ERR=$'\033[31m'; C_STEP=$'\033[36m'; C_DIM=$'\033[2m'; C_END=$'\033[0m'
else
    C_OK=""; C_WARN=""; C_ERR=""; C_STEP=""; C_DIM=""; C_END=""
fi
step()  { printf '%s\n' "${C_STEP}==> ${C_END}${*}"; }
ok()    { printf '%s\n' "${C_OK}  ✔ ${C_END}${*}"; }
warn()  { printf '%s\n' "${C_WARN}  ⚠ ${C_END}${*}"; }
die()   { printf '%s\n' "${C_ERR}  ✘ ${C_END}${*}" >&2; exit 1; }

step "检测系统(仅支持 Linux)"
ARCH=$(uname -m)
case $ARCH in
    x86_64)  ARCH="amd64";;
    aarch64) ARCH="arm64";;
    armv7l)  ARCH="armv7";;
    *) die "不支持的架构: $ARCH";;
esac
ok "架构 linux/${ARCH}"

AGENT_URL="${SERVER_URL}/download/agent/linux/${ARCH}"
step "下载 Agent 并校验 SHA256"
printf '%s\n' "${C_DIM}    ${AGENT_URL}${C_END}"
curl -fL --progress-bar -o /tmp/xprobe-agent "${AGENT_URL}"
curl -fsSL -o /tmp/xprobe-agent.sha256 "${AGENT_URL}.sha256" 2>/dev/null || true
if [[ -s /tmp/xprobe-agent.sha256 ]]; then
    EXPECT=$(cut -d' ' -f1 /tmp/xprobe-agent.sha256)
    ACTUAL=$(sha256sum /tmp/xprobe-agent | cut -d' ' -f1)
    [[ "$EXPECT" == "$ACTUAL" ]] || die "SHA256 校验失败"
    ok "校验通过"
else
    warn "未获取到校验文件, 跳过"
fi
chmod +x /tmp/xprobe-agent
mv /tmp/xprobe-agent /usr/local/bin/xprobe-agent

step "创建非特权用户 probe 与目录"
id probe &>/dev/null || useradd -r -s /usr/sbin/nologin probe
mkdir -p /etc/xprobe-agent /var/lib/xprobe-agent
ok "用户与目录就绪"

step "生成 install_salt(主机指纹因子)"
INSTALL_SALT=$(openssl rand -hex 32)
ok "已生成"

step "获取 Server 证书指纹(证书 Pinning)"
if [[ -z "$CERT_FP" ]]; then
    CERT_FP=$(curl -fsSk --connect-timeout 10 "${SERVER_URL}/api/v1/server-cert" \
        | grep -o '"fingerprint"[[:space:]]*:[[:space:]]*"[^"]*"' | cut -d'"' -f4 || true)
fi
if [[ -z "$CERT_FP" ]]; then
    die "无法获取证书指纹: 请加参数 --cert-fingerprint <指纹> 重试(指纹见 Server 的 /api/v1/server-cert)"
fi
ok "指纹 ${CERT_FP:0:16}…"

step "解锁 unprivileged ICMP(sysctl)"
echo 'net.ipv4.ping_group_range = 0 2147483647' > /etc/sysctl.d/99-xprobe-agent.conf
sysctl --system >/dev/null 2>&1
ok "已写入 /etc/sysctl.d/99-xprobe-agent.conf"

step "写入配置(/etc/xprobe-agent/config.yml, 权限 600)"
cat > /etc/xprobe-agent/config.yml << EOF
server: "${SERVER_URL}"
register_code: "${REGISTER_CODE}"
install_salt: "${INSTALL_SALT}"
server_cert_fingerprints:
  - "${CERT_FP}"
state_file: "/var/lib/xprobe-agent/state.json"
report_interval: 3
config_sync_interval: 3600
ping_method: "auto"
EOF
chown -R probe:probe /etc/xprobe-agent /var/lib/xprobe-agent
chmod 600 /etc/xprobe-agent/config.yml
ok "配置完成(注册码将在首次注册后自动清除)"

step "尝试 setcap(ICMP, 可选)"
if setcap cap_net_raw+ep /usr/local/bin/xprobe-agent 2>/dev/null; then
    ok "CAP_NET_RAW 已设置"
else
    warn "setcap 失败, 将尝试 unprivileged ICMP 或降级 TCP Ping"
fi

step "安装 systemd 服务"
cat > /etc/systemd/system/xprobe-agent.service << 'EOF'
[Unit]
Description=XProbe Agent
After=network.target

[Service]
Type=simple
User=probe
Group=probe
AmbientCapabilities=CAP_NET_RAW
CapabilityBoundingSet=CAP_NET_RAW
ExecStart=/usr/local/bin/xprobe-agent --config /etc/xprobe-agent/config.yml
Restart=always
RestartSec=5
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ReadWritePaths=/etc/xprobe-agent /var/lib/xprobe-agent

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable xprobe-agent >/dev/null 2>&1
systemctl start xprobe-agent
ok "服务已启动"

sleep 3
step "自检"
if [ "$(systemctl is-active xprobe-agent)" != "active" ]; then
    warn "服务未运行, 查看日志: journalctl -u xprobe-agent -n 50 --no-pager"
else
    ok "Agent 运行中, 正在注册并上报"
fi

echo
printf '%s\n' "${C_STEP}──────────────────────────────────────────────${C_END}"
echo "  XProbe Agent 安装完成"
printf '%s\n' "${C_STEP}──────────────────────────────────────────────${C_END}"
echo "  配置文件   : /etc/xprobe-agent/config.yml"
echo "  查看日志   : journalctl -u xprobe-agent -f"
echo "  重启服务   : systemctl restart xprobe-agent"
echo "  卸载       : systemctl disable --now xprobe-agent && userdel probe"
echo "  面板确认   : 几秒后应出现在仪表盘(在线)"
printf '%s\n' "${C_STEP}──────────────────────────────────────────────${C_END}"
