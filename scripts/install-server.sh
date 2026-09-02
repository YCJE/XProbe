#!/usr/bin/env bash
# XProbe Server 一键安装(设计文档 10.8 M6):
#   curl -fsSL https://raw.githubusercontent.com/YCJE/XProbe/main/scripts/install-server.sh | bash
# 默认从 GitHub Releases 下载; 二进制含前端 embed, 自包含。
set -euo pipefail

VERSION="${XPROBE_VERSION:-latest}"
BASE_URL="https://github.com/YCJE/XProbe/releases"
DATA_DIR="/var/lib/xprobe-server"
CONFIG_DIR="/etc/xprobe-server"

ARCH=$(uname -m)
case $ARCH in
    x86_64)  ARCH="amd64";;
    aarch64) ARCH="arm64";;
    *) echo "不支持的架构: $ARCH"; exit 1;;
esac

if [[ "$VERSION" == "latest" ]]; then
    VERSION=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "${BASE_URL}/latest" | grep -o '[^/]*$' || true)
fi
[[ -z "$VERSION" ]] && { echo "无法确定最新版本号, 请检查网络或用 XPROBE_VERSION=vX.Y.Z 指定"; exit 1; }
URL="${BASE_URL}/download/${VERSION}/xprobe-server-linux-${ARCH}"
echo "下载 XProbe Server ${VERSION}: ${URL}"
curl -fsSL -o /tmp/xprobe-server "${URL}"
curl -fsSL -o /tmp/xprobe-server.sha256 "${URL}.sha256" 2>/dev/null && \
    echo "$(cat /tmp/xprobe-server.sha256)  /tmp/xprobe-server" | sha256sum -c -
chmod +x /tmp/xprobe-server
mv /tmp/xprobe-server /usr/local/bin/xprobe-server

mkdir -p "$DATA_DIR" "$CONFIG_DIR"
if [[ ! -f "$CONFIG_DIR/config.yml" ]]; then
    cat > "$CONFIG_DIR/config.yml" << EOF
listen: ":443"
data_dir: "${DATA_DIR}"
tls:
  cert: ""   # 留空: 首次启动生成自签证书(指纹见启动日志 / /api/v1/server-cert)
  key: ""
auth:
  jwt_secret: ""      # 留空: 首次启动生成并写回; 建议以 PROBE_JWT_SECRET 环境变量注入
  cookie_secure: true
EOF
    chmod 600 "$CONFIG_DIR/config.yml"
fi

id xprobe &>/dev/null || useradd -r -s /usr/sbin/nologin xprobe
chown -R xprobe:xprobe "$DATA_DIR"
# 服务以 xprobe 运行, 配置必须可读(审查 HIGH #2: 否则启动即崩溃循环)
chown xprobe:xprobe "$CONFIG_DIR/config.yml"

cat > /etc/systemd/system/xprobe-server.service << EOF
[Unit]
Description=XProbe Server
After=network.target

[Service]
Type=simple
User=xprobe
Group=xprobe
EnvironmentFile=-/etc/xprobe-server/env
ExecStart=/usr/local/bin/xprobe-server --config ${CONFIG_DIR}/config.yml
Restart=always
RestartSec=5
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ReadWritePaths=${DATA_DIR}

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable xprobe-server
systemctl start xprobe-server
echo "Server 已安装并启动: systemctl status xprobe-server"
echo "首次访问 https://<host>/ 完成管理员初始化; 证书指纹: curl -k https://<host>/api/v1/server-cert"
