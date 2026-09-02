#!/usr/bin/env bash
# XProbe Agent 一键安装(设计文档 8.3):
#   curl -fsSL https://your-server.com/install.sh | bash -s -- --server https://your-server.com --code ABC123XY
# 可选: --cert-fingerprint <sha256>(自签场景带外传递, 否则脚本从 /api/v1/server-cert 获取)
set -euo pipefail

SERVER_URL=""
REGISTER_CODE=""
CERT_FP=""
while [[ $# -gt 0 ]]; do
    case $1 in
        --server) SERVER_URL="$2"; shift 2;;
        --code) REGISTER_CODE="$2"; shift 2;;
        --cert-fingerprint) CERT_FP="$2"; shift 2;;
        *) echo "Unknown arg: $1"; exit 1;;
    esac
done
[[ -z "$SERVER_URL" || -z "$REGISTER_CODE" ]] && { echo "需要 --server 与 --code"; exit 1; }

# 1. 检测系统(v1 仅 Linux, 设计文档 4.1)
ARCH=$(uname -m)
case $ARCH in
    x86_64)  ARCH="amd64";;
    aarch64) ARCH="arm64";;
    armv7l)  ARCH="armv7";;
    *) echo "不支持的架构: $ARCH"; exit 1;;
esac

# 2. 从 Server 内嵌分发端点下载并校验 SHA256
AGENT_URL="${SERVER_URL}/download/agent/linux/${ARCH}"
echo "下载 Agent: ${AGENT_URL}"
curl -fsSL -o /tmp/xprobe-agent "${AGENT_URL}"
curl -fsSL -o /tmp/xprobe-agent.sha256 "${AGENT_URL}.sha256"
EXPECT=$(cut -d' ' -f1 /tmp/xprobe-agent.sha256)
ACTUAL=$(sha256sum /tmp/xprobe-agent | cut -d' ' -f1)
[[ "$EXPECT" == "$ACTUAL" ]] || { echo "SHA256 校验失败: $EXPECT != $ACTUAL"; exit 1; }
chmod +x /tmp/xprobe-agent
mv /tmp/xprobe-agent /usr/local/bin/xprobe-agent

# 3. 创建非特权用户与目录(S3/S8)
id probe &>/dev/null || useradd -r -s /usr/sbin/nologin probe
mkdir -p /etc/xprobe-agent /var/lib/xprobe-agent

# 4. 生成安装盐(参与主机指纹, 升级/重装保持不变则指纹不变)
INSTALL_SALT=$(openssl rand -hex 32)

# 5. 带外获取 Server 证书指纹(Agent Pinning, 设计文档 4.2/7.5)
if [[ -z "$CERT_FP" ]]; then
    CERT_FP=$(curl -fsSL "${SERVER_URL}/api/v1/server-cert" | grep -o '"fingerprint"[[:space:]]*:[[:space:]]*"[^"]*"' | cut -d'"' -f4 || true)
fi
[[ -z "$CERT_FP" ]] && { echo "警告: 未能获取证书指纹; 如 Server 使用自签证书请以 --cert-fingerprint 重试"; }

# 6. 解锁 unprivileged ICMP(v1.3: 否则降级链第二级在多数发行版直接失效)
echo 'net.ipv4.ping_group_range = 0 2147483647' > /etc/sysctl.d/99-xprobe-agent.conf
sysctl --system >/dev/null

# 7. 写配置(权限 600, 属主 probe)
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

# 8. 尝试 setcap(ICMP privileged)
if setcap cap_net_raw+ep /usr/local/bin/xprobe-agent 2>/dev/null; then
    echo "ICMP Ping 已启用 (CAP_NET_RAW)"
else
    echo "setcap 失败, 将尝试 unprivileged ICMP 或降级 TCP Ping"
fi

# 9. systemd 服务(安全加固)
cat > /etc/systemd/system/xprobe-agent.service << 'EOF'
[Unit]
Description=XProbe Agent
After=network.target

[Service]
Type=simple
User=probe
Group=probe
ExecStart=/usr/local/bin/xprobe-agent --config /etc/xprobe-agent/config.yml
Restart=always
RestartSec=5
# NoNewPrivileges 下 file capabilities 失效, ICMP 用 AmbientCapabilities 授予
AmbientCapabilities=CAP_NET_RAW
CapabilityBoundingSet=CAP_NET_RAW
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ReadWritePaths=/etc/xprobe-agent /var/lib/xprobe-agent

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable xprobe-agent
systemctl start xprobe-agent
echo "Agent 已安装并启动。查看状态: systemctl status xprobe-agent"
