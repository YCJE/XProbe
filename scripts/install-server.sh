#!/usr/bin/env bash
# ============================================================
#  XProbe Server 一键安装脚本
#  用法:
#    curl -fsSL https://raw.githubusercontent.com/YCJE/XProbe/main/scripts/install-server.sh | bash
#  可选: XPROBE_VERSION=v0.1.0 指定版本
#  流程: 版本探测 → 下载+校验 → 安装 → 配置 → systemd → 自检
# ============================================================
set -euo pipefail

VERSION="${XPROBE_VERSION:-latest}"
BASE_URL="https://github.com/YCJE/XProbe/releases"
DATA_DIR="/var/lib/xprobe-server"
CONFIG_DIR="/etc/xprobe-server"
BIN=/usr/local/bin/xprobe-server

# ---------- 输出工具(彩色, 非终端时自动退化为纯文本) ----------
if [ -t 2 ]; then
    C_OK=$'\033[32m'; C_WARN=$'\033[33m'; C_ERR=$'\033[31m'; C_STEP=$'\033[36m'; C_DIM=$'\033[2m'; C_END=$'\033[0m'
else
    C_OK=""; C_WARN=""; C_ERR=""; C_STEP=""; C_DIM=""; C_END=""
fi
step()  { printf '%s\n' "${C_STEP}==> ${C_END}${*}"; }
ok()    { printf '%s\n' "${C_OK}  ✔ ${C_END}${*}"; }
warn()  { printf '%s\n' "${C_WARN}  ⚠ ${C_END}${*}"; }
die()   { printf '%s\n' "${C_ERR}  ✘ ${C_END}${*}" >&2; exit 1; }

# ---------- 0. 环境检测 ----------
step "检测系统环境"
[ "$(id -u)" -eq 0 ] || die "请以 root 运行(需要写 /usr/local/bin 与 systemd)"
ARCH=$(uname -m)
case $ARCH in
    x86_64)  ARCH="amd64";;
    aarch64) ARCH="arm64";;
    *) die "不支持的架构: $ARCH(目前支持 x86_64 / aarch64)";;
esac
ok "架构 linux/${ARCH}"

# ---------- 1. 版本探测 ----------
step "探测最新版本"
if [[ "$VERSION" == "latest" ]]; then
    VERSION=$(curl -fsSL --connect-timeout 15 "https://api.github.com/repos/YCJE/XProbe/releases/latest" \
        | grep -o '"tag_name"[[:space:]]*:[[:space:]]*"[^"]*"' | cut -d'"' -f4 || true)
fi
if [[ -z "$VERSION" ]]; then
    cat >&2 <<'MSG'
  ✘ 仓库还没有发布任何 Release。可选方案:
      1) 推送 v* 标签触发 CI 自动发布( git tag v0.1.0 && git push origin v0.1.0 )
      2) 用 Docker 部署: docker compose up -d
      3) 本地 make release 后手动上传, 并以 XPROBE_VERSION=vX.Y.Z 重试
MSG
    exit 1
fi
ok "目标版本 ${VERSION}"

# ---------- 2. 下载 + SHA256 校验 ----------
URL="${BASE_URL}/download/${VERSION}/xprobe-server-linux-${ARCH}"
step "下载 Server 二进制"
printf '%s\n' "${C_DIM}    ${URL}${C_END}"
curl -fL --progress-bar -o /tmp/xprobe-server "${URL}"
ok "下载完成"
step "校验 SHA256"
if curl -fsSL -o /tmp/xprobe-server.sha256 "${URL}.sha256" 2>/dev/null; then
    EXPECT=$(cut -d' ' -f1 /tmp/xprobe-server.sha256)
    ACTUAL=$(sha256sum /tmp/xprobe-server | cut -d' ' -f1)
    [[ "$EXPECT" == "$ACTUAL" ]] || die "校验失败: $EXPECT != $ACTUAL"
    ok "校验通过"
else
    warn "未提供 .sha256 文件, 跳过校验"
fi

# ---------- 3. 安装二进制 ----------
step "安装到 /usr/local/bin/xprobe-server"
chmod +x /tmp/xprobe-server
mv /tmp/xprobe-server "$BIN"
ok "安装完成"

# ---------- 4. 配置 ----------
step "写入配置(已存在则保留原配置)"
mkdir -p "$DATA_DIR" "$CONFIG_DIR"
if [[ ! -f "$CONFIG_DIR/config.yml" ]]; then
    cat > "$CONFIG_DIR/config.yml" << EOF
listen: ":443"            # 监听地址; 443 被占用可改为其他端口
data_dir: "${DATA_DIR}"   # SQLite 数据目录
tls:
  cert: ""                # 留空: 首次启动自动生成自签证书(建议后续替换为正式证书)
  key: ""
auth:
  jwt_secret: ""          # 留空: 首次启动生成并写回; Docker 建议用 PROBE_JWT_SECRET 注入
  cookie_secure: true
EOF
    chmod 600 "$CONFIG_DIR/config.yml"
    ok "新配置已写入 ${CONFIG_DIR}/config.yml"
else
    ok "检测到已有配置, 保持不变"
fi

# ---------- 5. 服务账号与 systemd ----------
step "创建系统服务"
id xprobe &>/dev/null || useradd -r -s /usr/sbin/nologin xprobe
chown -R xprobe:xprobe "$DATA_DIR"
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
ExecStart=$BIN --config ${CONFIG_DIR}/config.yml
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
systemctl enable xprobe-server >/dev/null 2>&1
systemctl restart xprobe-server
ok "服务已启动(xprobe 用户, 非 root)"

# ---------- 6. 自检 + 安装摘要 ----------
sleep 2
step "自检"
if [ "$(systemctl is-active xprobe-server)" != "active" ]; then
    warn "服务未进入 active 状态, 查看日志: journalctl -u xprobe-server -n 50 --no-pager"
else
    ok "服务运行中"
fi
FP=$(curl -fsSk --connect-timeout 5 "https://127.0.0.1/api/v1/server-cert" 2>/dev/null \
    | grep -o '"fingerprint"[[:space:]]*:[[:space:]]*"[^"]*"' | cut -d'"' -f4 || true)

LOCAL_IP=$(hostname -I 2>/dev/null | awk '{print $1}')
[ -z "$LOCAL_IP" ] && LOCAL_IP="<服务器IP>"
echo
printf '%s\n' "${C_STEP}──────────────────────────────────────────────${C_END}"
echo "  XProbe Server ${VERSION} 安装完成"
printf '%s\n' "${C_STEP}──────────────────────────────────────────────${C_END}"
echo "  面板地址   : https://${LOCAL_IP}/  (浏览器会提示自签证书, 属正常)"
echo "  证书指纹   : ${FP:-<见下方命令>}"
[ -z "$FP" ] && echo "               curl -k https://127.0.0.1/api/v1/server-cert"
echo "  数据目录   : ${DATA_DIR}"
echo "  配置文件   : ${CONFIG_DIR}/config.yml"
echo "  查看日志   : journalctl -u xprobe-server -f"
echo "  重启服务   : systemctl restart xprobe-server"
echo
echo "  下一步:"
echo "    1. 云安全组/防火墙放行 443/tcp"
echo "    2. 浏览器打开面板, 创建管理员账号(无默认密码)"
echo "    3. 设置 → 注册码 → 生成, 到被控服务器安装 Agent"
printf '%s\n' "${C_STEP}──────────────────────────────────────────────${C_END}"
