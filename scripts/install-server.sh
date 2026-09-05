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
CERT_PATH=""   # 可选: 已有证书 fullchain.pem(如 certbot 签发)
KEY_PATH=""    # 可选: 对应 privkey.pem
DOMAIN=""      # 可选: 仅用于摘要展示

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
while [[ $# -gt 0 ]]; do
    case $1 in
        --cert) CERT_PATH="$2"; shift 2;;
        --key) KEY_PATH="$2"; shift 2;;
        --domain) DOMAIN="$2"; shift 2;;
        *) shift;;
    esac
done
if [[ -n "$CERT_PATH" && -n "$KEY_PATH" ]]; then
    [[ -f "$CERT_PATH" && -f "$KEY_PATH" ]] || die "证书文件不存在: $CERT_PATH / $KEY_PATH"
fi
ok "架构 linux/${ARCH}${DOMAIN:+ · 域名 $DOMAIN}"

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
TLS_CERT_LINE='  cert: ""                # 留空: 首次启动自动生成自签证书(建议后续替换为正式证书)'
TLS_KEY_LINE='  key: ""'
if [[ -n "$CERT_PATH" && -n "$KEY_PATH" ]]; then
    TLS_CERT_LINE="  cert: \"${CERT_PATH}\"    # 已配置外部证书(热加载: 续期后自动生效, 无需重启)"
    TLS_KEY_LINE="  key: \"${KEY_PATH}\""
fi
if [[ ! -f "$CONFIG_DIR/config.yml" ]]; then
    cat > "$CONFIG_DIR/config.yml" << EOF
listen: ":443"            # 监听地址; 443 被占用可改为其他端口
data_dir: "${DATA_DIR}"   # SQLite 数据目录
tls:
${TLS_CERT_LINE}
${TLS_KEY_LINE}
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
if [[ -n "$CERT_PATH" ]]; then
    # 服务用户必须能读证书(热加载每次握手都会读)
    chown xprobe:xprobe "$CERT_PATH" "$KEY_PATH"
fi
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
# 非 root 绑定 443 需要(与 Agent 的 CAP_NET_RAW 同思路)
AmbientCapabilities=CAP_NET_BIND_SERVICE
CapabilityBoundingSet=CAP_NET_BIND_SERVICE
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
    echo
    printf '%s\n' "${C_ERR}  ✘ 服务未运行, 最近日志:${C_END}"
    journalctl -u xprobe-server -n 12 --no-pager | sed 's/^/    /' || true
    cat >&2 <<'MSG'

  常见原因:
    · bind: permission denied → 旧版服务单元缺少能力授予, 重新运行本脚本即可修复
    · bind: address already in use → 443 被占用, 修改 config.yml 的 listen 后重启
    · 前端 404 → 二进制未内嵌前端(源码编译请先 make build-frontend)
MSG
    exit 1
fi
ok "服务运行中"

# ---------- 7. TLS 向导(域名与 HTTPS 证书) ----------
# 交互判定: 终端直接可用=1; 管道但有 /dev/tty(SSH)=2; 全无=0(仅参数模式)
INTERACTIVE=0
if [ -t 0 ]; then INTERACTIVE=1
elif [ -e /dev/tty ]; then INTERACTIVE=2
fi
ASK() {
    REPLY=""
    if [ "$INTERACTIVE" -ge 1 ]; then
        read -r -p "$1" REPLY < /dev/tty 2>/dev/null || REPLY=""
    fi
}

CERT_PATH=""
KEY_PATH=""
TLS_MODE="skip"
if [ -n "$DOMAIN" ]; then
    TLS_MODE="certbot"                       # 参数模式: 非交互直接签发
elif [ "$SKIP_TLS" = "1" ]; then
    TLS_MODE="skip"
elif [ "$INTERACTIVE" -ge 1 ]; then
    echo
    step "TLS 向导: 配置域名与 HTTPS 证书"
    echo "  ${C_DIM}配置后浏览器无证书告警, Agent 安装命令自动带上域名与新指纹。${C_END}"
    ASK "  是否现在配置? [Y/n]: "
    case "${REPLY:-Y}" in
        [nN]*) TLS_MODE="skip";;
        *)     TLS_MODE="certbot";;
    esac
else
    SKIP_HINT=1
fi

if [ "$TLS_MODE" = "certbot" ]; then
    step "TLS: 安装 certbot"
    if command -v certbot >/dev/null 2>&1; then
        ok "certbot 已安装"
    else
        if command -v apt-get >/dev/null 2>&1; then
            apt-get update -qq && apt-get install -y -qq certbot && ok "已安装(apt)"
        elif command -v dnf >/dev/null 2>&1; then
            dnf install -y epel-release >/dev/null 2>&1 || true
            dnf install -y -q certbot && ok "已安装(dnf)"
        else
            warn "无法自动安装 certbot(不支持的包管理器)"
            echo "  ${C_DIM}手动: snap install core && snap install --classic certbot${C_END}"
            TLS_MODE="skip"
        fi
    fi
fi

if [ "$TLS_MODE" = "certbot" ]; then
    if [ -z "$DOMAIN" ] && [ "$INTERACTIVE" -ge 1 ]; then
        ASK "  输入域名(需已解析到本机, 如 probe.example.com): "
        DOMAIN="$REPLY"
    fi
    [ -z "$DOMAIN" ] && { warn "未输入域名, 跳过 TLS"; TLS_MODE="skip"; }
fi

if [ "$TLS_MODE" = "certbot" ]; then
    LOCAL_IP=$(hostname -I 2>/dev/null | awk '{print $1}')
    DOMAIN_IP=$(getent hosts "$DOMAIN" 2>/dev/null | awk '{print $1}' | head -1)
    if [ -n "$DOMAIN_IP" ] && [ "$DOMAIN_IP" != "$LOCAL_IP" ]; then
        warn "域名 $DOMAIN 解析到 $DOMAIN_IP, 与本机 $LOCAL_IP 不一致"
        ASK "  仍要继续签发? [y/N]: "
        case "${REPLY:-N}" in [yY]*) ;; *) TLS_MODE="skip";; esac
    else
        ok "DNS 解析正常"
    fi
fi

if [ "$TLS_MODE" = "certbot" ]; then
    step "签发证书(standalone, 需 80 端口空闲且已放行)"
    if [ "$INTERACTIVE" -ge 1 ]; then
        ASK "  证书到期提醒邮箱(可回车跳过): "
        EMAIL="$REPLY"
    fi
    CERTBOT_ARGS="certonly --standalone -d $DOMAIN --agree-tos --keep-until-expiring"
    if [ -n "$EMAIL" ]; then CERTBOT_ARGS="$CERTBOT_ARGS -m $EMAIL"; else CERTBOT_ARGS="$CERTBOT_ARGS --register-unsafely-without-email"; fi
    if certbot $CERTBOT_ARGS; then
        ok "证书已签发"
    else
        warn "签发失败(常见: 80 未放行/域名未解析)。可修复后重跑本脚本, 先跳过 TLS"
        TLS_MODE="skip"
    fi
fi

if [ "$TLS_MODE" = "certbot" ]; then
    step "部署证书与续期钩子"
    mkdir -p /var/lib/xprobe-server/certs
    cp -f "/etc/letsencrypt/live/$DOMAIN/fullchain.pem" /var/lib/xprobe-server/certs/
    cp -f "/etc/letsencrypt/live/$DOMAIN/privkey.pem"  /var/lib/xprobe-server/certs/
    chown -R xprobe:xprobe /var/lib/xprobe-server/certs
    cat > /etc/letsencrypt/renewal-hooks/deploy/xprobe.sh << 'HOOK'
#!/usr/bin/env bash
cp -f "$RENEWED_DIR/fullchain.pem" /var/lib/xprobe-server/certs/
cp -f "$RENEWED_DIR/privkey.pem"  /var/lib/xprobe-server/certs/
chown -R xprobe:xprobe /var/lib/xprobe-server/certs
HOOK
    chmod +x /etc/letsencrypt/renewal-hooks/deploy/xprobe.sh
    ok "deploy hook 已安装(续期自动同步)"
    sed -i "s|^  cert: .*|  cert: /var/lib/xprobe-server/certs/fullchain.pem|; s|^  key: .*|  key: /var/lib/xprobe-server/certs/privkey.pem|" "$CONFIG_DIR/config.yml"
    chown xprobe:xprobe "$CONFIG_DIR/config.yml"
    systemctl restart xprobe-server
    sleep 2
    [ "$(systemctl is-active xprobe-server)" = "active" ] || die "配置证书后服务未运行: journalctl -u xprobe-server -n 30"
    ok "HTTPS(域名证书)已生效"
fi

if [ "$TLS_MODE" = "skip" ] && [ "${SKIP_HINT:-0}" = "1" ]; then
    echo "  ${C_DIM}已跳过 TLS 向导(非交互); 后续配置见 README「配置域名与 HTTPS 证书」${C_END}"
fi

FP=$(curl -fsSk --connect-timeout 5 "https://127.0.0.1/api/v1/server-cert" 2>/dev/null \
    | grep -o '"fingerprint"[[:space:]]*:[[:space:]]*"[^"]*"' | cut -d'"' -f4 || true)

LOCAL_IP=$(hostname -I 2>/dev/null | awk '{print $1}')
[ -z "$LOCAL_IP" ] && LOCAL_IP="<服务器IP>"
PANEL_HOST="${DOMAIN:-${LOCAL_IP}}"
AGENT_CMD="curl -fsSL https://raw.githubusercontent.com/YCJE/XProbe/main/scripts/install-agent.sh | bash -s -- --server https://${PANEL_HOST} --code <注册码>${FP:+ --cert-fingerprint $FP}"

echo
printf '%s\n' "${C_STEP}──────────────────────────────────────────────${C_END}"
echo "  XProbe Server ${VERSION} 安装完成"
printf '%s\n' "${C_STEP}──────────────────────────────────────────────${C_END}"
if [[ -n "$CERT_PATH" || "$TLS_MODE" = "certbot" ]]; then
    echo "  面板地址   : https://${PANEL_HOST}/  (正式证书, 浏览器无告警)"
else
    echo "  面板地址   : https://${PANEL_HOST}/  (自签证书, 浏览器有告警; 可重跑脚本配置域名证书)"
fi
echo "  证书指纹   : ${FP:-<见下方命令>}"
[ -z "$FP" ] && echo "               curl -k https://127.0.0.1/api/v1/server-cert"
echo "  数据目录   : ${DATA_DIR}"
echo "  配置文件   : ${CONFIG_DIR}/config.yml"
echo "  查看日志   : journalctl -u xprobe-server -f"
echo
echo "  Agent 安装 : 到被控服务器执行(注册码在面板「设置 → 注册码」生成):"
echo "    ${AGENT_CMD}"
echo
echo "  下一步:"
echo "    1. 云安全组/防火墙放行 443/tcp"
echo "    2. 浏览器打开面板, 创建管理员账号(无默认密码)"
echo "    3. 生成注册码, 用上方命令安装 Agent"
printf '%s\n' "${C_STEP}──────────────────────────────────────────────${C_END}"
