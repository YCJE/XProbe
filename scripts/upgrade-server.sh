#!/usr/bin/env bash
# ============================================================
#  XProbe Server 一键升级脚本
#  用法:
#    curl -fsSL https://raw.githubusercontent.com/YCJE/XProbe/main/scripts/upgrade-server.sh | bash
#  可选: XPROBE_VERSION=vX.Y.Z 指定目标版本(默认 latest)
#  行为: 自动备份数据库(sqlite3 .backup 在线快照) → 下载新版 → 原地替换 → 重启
#  保留: 配置/数据/证书均不动; 旧二进制保留为 .bak 可随时回滚
# ============================================================
set -euo pipefail

VERSION="${XPROBE_VERSION:-latest}"
BASE_URL="https://github.com/YCJE/XProbe/releases"
DATA_DIR="/var/lib/xprobe-server"
BIN=/usr/local/bin/xprobe-server

if [ -t 2 ]; then
    C_OK=$'\033[32m'; C_WARN=$'\033[33m'; C_ERR=$'\033[31m'; C_STEP=$'\033[36m'; C_DIM=$'\033[2m'; C_END=$'\033[0m'
else
    C_OK=""; C_WARN=""; C_ERR=""; C_STEP=""; C_DIM=""; C_END=""
fi
step()  { printf '%s\n' "${C_STEP}==> ${C_END}${*}"; }
ok()    { printf '%s\n' "${C_OK}  ✔ ${C_END}${*}"; }
warn()  { printf '%s\n' "${C_WARN}  ⚠ ${C_END}${*}"; }
die()   { printf '%s\n' "${C_ERR}  ✘ ${C_END}${*}" >&2; exit 1; }

[ "$(id -u)" -eq 0 ] || die "请以 root 运行"
[ -f "$BIN" ] || die "未检测到已安装的 Server($BIN 不存在), 请先运行一键安装"
systemctl list-unit-files 2>/dev/null | grep -q xprobe-server || die "未检测到 xprobe-server 服务, 请先运行一键安装"

step "获取当前与新版本号"
OLD_VERSION=$("$BIN" --version 2>/dev/null | tail -1 | awk '{print $NF}')
if [[ "$VERSION" == "latest" ]]; then
    VERSION=$(curl -fsSL --connect-timeout 15 "https://api.github.com/repos/YCJE/XProbe/releases/latest" \
        | grep -o '"tag_name"[[:space:]]*:[[:space:]]*"[^"]*"' | cut -d'"' -f4 || true)
fi
[[ -z "$VERSION" ]] && die "无法确定目标版本(仓库无 Release?), 可用 XPROBE_VERSION=vX.Y.Z 指定"
NEW_VERSION=$VERSION
ok "当前 ${OLD_VERSION:-未知} → 目标 ${NEW_VERSION}"
if [ "$OLD_VERSION" = "$NEW_VERSION" ]; then
    ok "已是目标版本, 无需升级"
    exit 0
fi

ARCH=$(uname -m); case $ARCH in x86_64) ARCH=amd64;; aarch64) ARCH=arm64;; *) die "不支持架构 $ARCH";; esac
URL="${BASE_URL}/download/${NEW_VERSION}/xprobe-server-linux-${ARCH}"

step "下载数据库备份(升级前快照)"
if command -v sqlite3 >/dev/null 2>&1; then
    BK="${DATA_DIR}/backups/xprobe-pre-upgrade-$(date +%Y%m%d-%H%M%S).db"
    mkdir -p "${DATA_DIR}/backups"
    sqlite3 "${DATA_DIR}/xprobe.db" ".backup '${BK}'"
    ok "快照 ${BK}"
else
    warn "未安装 sqlite3, 跳过备份(建议: apt install sqlite3)"
fi

step "下载新版并校验"
curl -fL --progress-bar -o /tmp/xprobe-server.new "${URL}"
if curl -fsSL -o /tmp/xprobe-server.new.sha256 "${URL}.sha256" 2>/dev/null; then
    EXPECT=$(cut -d' ' -f1 /tmp/xprobe-server.new.sha256)
    ACTUAL=$(sha256sum /tmp/xprobe-server.new | cut -d' ' -f1)
    [[ "$EXPECT" == "$ACTUAL" ]] || die "SHA256 校验失败"
    ok "校验通过"
else
    warn "未提供 .sha256, 跳过校验"
fi

step "替换二进制并重启服务"
systemctl stop xprobe-server
cp "$BIN" "${BIN}.bak"   # 保留旧版用于回滚
mv /tmp/xprobe-server.new "$BIN"
chmod 755 "$BIN"
systemctl start xprobe-server
sleep 2
[ "$(systemctl is-active xprobe-server)" = "active" ] || die "升级后服务未运行! 回滚: cp ${BIN}.bak $BIN && systemctl start xprobe-server"
ok "服务运行中"

NEW_RUNNING=$("$BIN" --version 2>/dev/null | tail -1 | awk '{print $NF}')
echo
printf '%s\n' "${C_STEP}──────────────────────────────────────────────${C_END}"
echo "  XProbe Server 升级完成"
printf '%s\n' "${C_STEP}──────────────────────────────────────────────${C_END}"
echo "  版本   : ${OLD_VERSION:-未知} → ${NEW_RUNNING}"
echo "  配置   : 未改动   数据: 未改动   证书: 未改动"
echo "  备份   : ${BK:-<跳过>}"
echo "  回滚   : systemctl stop xprobe-server && cp ${BIN}.bak $BIN && systemctl start xprobe-server"
printf '%s\n' "${C_STEP}──────────────────────────────────────────────${C_END}"
