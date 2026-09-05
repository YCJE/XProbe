#!/usr/bin/env bash
# ============================================================
#  XProbe Server 一键卸载脚本
#  用法:
#    curl -fsSL https://raw.githubusercontent.com/YCJE/XProbe/main/scripts/uninstall-server.sh | bash
#    加 --purge 连同数据/配置/账号一起删除(默认保留, 重装后自动接上)
# ============================================================
set -euo pipefail

PURGE=0
for arg in "$@"; do
    [ "$arg" = "--purge" ] && PURGE=1
done

if [ -t 2 ]; then
    C_OK=$'\033[32m'; C_WARN=$'\033[33m'; C_ERR=$'\033[31m'; C_STEP=$'\033[36m'; C_DIM=$'\033[2m'; C_END=$'\033[0m'
else
    C_OK=""; C_WARN=""; C_ERR=""; C_STEP=""; C_DIM=""; C_END=""
fi
step()  { printf '%s\n' "${C_STEP}==> ${C_END}${*}"; }
ok()    { printf '%s\n' "${C_OK}  ✔ ${C_END}${*}"; }
die()   { printf '%s
' "${C_ERR}  ✘ ${C_END}${*}" >&2; exit 1; }
warn()  { printf '%s\n' "${C_WARN}  ⚠ ${C_END}${*}"; }

BIN=/usr/local/bin/xprobe-server
DATA_DIR=/var/lib/xprobe-server
CONFIG_DIR=/etc/xprobe-server

[ "$(id -u)" -eq 0 ] || { echo "请以 root 运行"; exit 1; }

if [ "$PURGE" = "1" ]; then
    warn "PURGE 模式: 数据库/配置/证书/账号将全部删除, 不可恢复!"
    if [ -t 0 ]; then
        read -r -p "确认继续? 输入 yes: " ANSWER
        [ "$ANSWER" = "yes" ] || { echo "已取消"; exit 0; }
    elif [ "${XPROBE_PURGE:-}" != "yes" ]; then
        die "非交互环境的 --purge 需显式确认: 加环境变量 XPROBE_PURGE=yes"
    fi
fi

step "停止并禁用服务"
systemctl stop xprobe-server 2>/dev/null && ok "已停止" || warn "服务本就未运行"
systemctl disable xprobe-server 2>/dev/null && ok "已禁用开机自启" || true

step "移除服务与二进制"
rm -f /etc/systemd/system/xprobe-server.service
systemctl daemon-reload
rm -f "$BIN" "${BIN}.bak"
ok "已移除"

if [ "$PURGE" = "1" ]; then
    step "PURGE: 删除数据与配置"
    rm -rf "$DATA_DIR" "$CONFIG_DIR"
    id xprobe &>/dev/null && userdel xprobe 2>/dev/null && ok "已删除 xprobe 用户" || true
    ok "全部数据已清除"
else
    step "保留数据(默认)"
    ok "数据目录 ${DATA_DIR} 与配置 ${CONFIG_DIR} 已保留"
    ok "重装同版本后自动接回历史数据; 彻底清除请追加 --purge"
fi

echo
printf '%s\n' "${C_STEP}──────────────────────────────────────────────${C_END}"
echo "  XProbe Server 卸载完成"
printf '%s\n' "${C_STEP}──────────────────────────────────────────────${C_END}"
echo "  已移除: 服务 / 二进制"
echo "  已保留: $( [ "$PURGE" = "1" ] && echo "无(purge)" || echo "数据 + 配置(重装可恢复)" )"
printf '%s\n' "${C_STEP}──────────────────────────────────────────────${C_END}"
