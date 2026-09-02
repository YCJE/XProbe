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
    # 通过 GitHub API 取最新发布版本(仓库无 Release 时给出可行动提示)
    VERSION=$(curl -fsSL --connect-timeout 15 "https://api.github.com/repos/YCJE/XProbe/releases/latest" \
        | grep -o '"tag_name"[[:space:]]*:[[:space:]]*"[^"]*"' | cut -d'"' -f4 || true)
fi
if [[ -z "$VERSION" ]]; then
    cat >&2 <<'MSG'
无法确定最新版本: 仓库可能还没有发布任何 Release。
可选方案:
  1) 推送 v* 标签触发 CI 自动发布( git tag v0.1.0 && git push origin v0.1.0 )
  2) 用 Docker 部署: docker compose up -d
  3) 本地 make release 后手动上传二进制, 并以 XPROBE_VERSION=vX.Y.Z 指定版本重试
MSG
    exit 1
fi
URL="${BASE_URL}/download/${VERSION}/xprobe-server-linux-${ARCH}"
echo "下载 XProbe Server ${VERSION}: ${URL}"
curl -fsSL -o /tmp/xprobe-server "${URL}"
if curl -fsSL -o /tmp/xprobe-server.sha256 "${URL}.sha256" 2>/dev/null; then
    EXPECT=$(cut -d' ' -f1 /tmp/xprobe-server.sha256)
    ACTUAL=$(sha256sum /tmp/xprobe-server | cut -d' ' -f1)
    [[ "$EXPECT" == "$ACTUAL" ]] || { echo "SHA256 校验失败: $EXPECT != $ACTUAL"; exit 1; }
else
    echo "警告: 服务器未提供 .sha256 校验文件, 跳过校验"
fi
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
