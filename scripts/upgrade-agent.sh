#!/usr/bin/env bash
# ============================================================
#  XProbe Agent 一键升级脚本(在被控服务器上执行)
#  用法:
#    curl -fsSL https://raw.githubusercontent.com/YCJE/XProbe/main/scripts/upgrade-agent.sh \
#      | bash -s -- --server https://your-server.com
#  行为: 下载新版 → 原地替换 → 重启; 配置/state.json/install_salt 均不动
#  (install_salt 不变则主机指纹不变, 不会触发 409); 旧二进制保留 .bak 可回滚
# ============================================================
set -euo pipefail

SERVER_URL=""
while [[ $# -gt 0 ]]; do
    case $1 in
        --server) SERVER_URL="$2"; shift 2;;
        *) echo "未知参数: $1"; exit 1;;
    esac
done
NEED_SERVER=0
[[ -z "$SERVER_URL" ]] && NEED_SERVER=1
BIN=/usr/local/bin/xprobe-agent
[ -f "$BIN" ] || { echo "未检测到已安装的 Agent($BIN 不存在), 请先运行一键安装"; exit 1; }
[ "$NEED_SERVER" = "1" ] && { echo "需要 --server https://your-server.com"; exit 1; }
case "$SERVER_URL" in
    https://*) ;;
    *) echo "S2: --server 必须 https://"; exit 1;;
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

ARCH=$(uname -m); case $ARCH in x86_64) ARCH=amd64;; aarch64) ARCH=arm64;; armv7l) ARCH=armv7;; *) die "不支持架构 $ARCH";; esac
OLD_VERSION=$("$BIN" --version 2>/dev/null | tail -1 | awk '{print $NF}')
URL="${SERVER_URL}/download/agent/linux/${ARCH}"

step "下载新版 Agent 并校验"
curl -fL --progress-bar -o /tmp/xprobe-agent.new "${URL}"
if curl -fsSL -o /tmp/xprobe-agent.new.sha256 "${URL}.sha256" 2>/dev/null; then
    EXPECT=$(cut -d' ' -f1 /tmp/xprobe-agent.new.sha256)
    ACTUAL=$(sha256sum /tmp/xprobe-agent.new | cut -d' ' -f1)
    [[ "$EXPECT" == "$ACTUAL" ]] || die "SHA256 校验失败"
    ok "校验通过"
else
    warn "服务器未提供 .sha256, 跳过校验"
fi

step "替换二进制并重启服务(配置/state.json 不动)"
systemctl stop xprobe-agent
cp "$BIN" "${BIN}.bak"
mv /tmp/xprobe-agent.new "$BIN"
chmod +x "$BIN"
setcap cap_net_raw+ep "$BIN" 2>/dev/null || warn "setcap 失败, 将走 unprivileged ICMP/TCP"
systemctl start xprobe-agent
sleep 3
[ "$(systemctl is-active xprobe-agent)" = "active" ] || die "升级后服务未运行! 回滚: cp ${BIN}.bak $BIN && systemctl start xprobe-agent"
NEW_VERSION=$("$BIN" --version 2>/dev/null | tail -1 | awk '{print $NF}')
ok "Agent 运行中"

echo
printf '%s\n' "${C_STEP}──────────────────────────────────────────────${C_END}"
echo "  XProbe Agent 升级完成"
printf '%s\n' "${C_STEP}──────────────────────────────────────────────${C_END}"
echo "  版本   : ${OLD_VERSION:-未知} → ${NEW_VERSION:-未知}"
echo "  配置   : 未改动(install_salt 不变, 主机指纹不变)"
echo "  回滚   : systemctl stop xprobe-agent && cp ${BIN}.bak $BIN && systemctl start xprobe-agent"
printf '%s\n' "${C_STEP}──────────────────────────────────────────────${C_END}"
