#!/usr/bin/env bash
# ============================================================
#  XProbe Agent 一键卸载脚本(在被控服务器上执行)
#  用法:
#    curl -fsSL https://raw.githubusercontent.com/YCJE/XProbe/main/scripts/uninstall-agent.sh | bash
#    加 --purge 连同配置/state.json/账号一起删除(默认保留)
#  注意: 卸载后面板中该 Agent 将显示离线; 数据仍在 Server 端
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
warn()  { printf '%s\n' "${C_WARN}  ⚠ ${C_END}${*}"; }

BIN=/usr/local/bin/xprobe-agent

[ "$(id -u)" -eq 0 ] || { echo "请以 root 运行"; exit 1; }

step "停止并禁用服务"
systemctl stop xprobe-agent 2>/dev/null && ok "已停止" || warn "服务本就未运行"
systemctl disable xprobe-agent 2>/dev/null && ok "已禁用开机自启" || true

step "移除服务与二进制"
rm -f /etc/systemd/system/xprobe-agent.service
systemctl daemon-reload
rm -f "$BIN" "${BIN}.bak"
rm -f /etc/sysctl.d/99-xprobe-agent.conf   # 收回 unprivileged ICMP 解锁
sysctl --system >/dev/null 2>&1 || true
ok "已移除"

if [ "$PURGE" = "1" ]; then
    step "PURGE: 删除配置与状态"
    rm -rf /etc/xprobe-agent /var/lib/xprobe-agent
    id probe &>/dev/null && userdel probe 2>/dev/null && ok "已删除 probe 用户" || true
    ok "本机痕迹已全部清除"
else
    step "保留配置与状态(默认)"
    ok "/etc/xprobe-agent 与 /var/lib/xprobe-agent 已保留"
    ok "彻底清除请追加 --purge"
fi

echo
printf '%s\n' "${C_STEP}──────────────────────────────────────────────${C_END}"
echo "  XProbe Agent 卸载完成"
printf '%s\n' "${C_STEP}──────────────────────────────────────────────${C_END}"
echo "  已移除: 服务 / 二进制 / sysctl 解锁"
echo "  已保留: $( [ "$PURGE" = "1" ] && echo "无(purge)" || echo "配置 + 状态(重装同 salt 可保持指纹不变)" )"
echo "  提示   : Server 面板中该机器将显示离线; 如需彻底移除请在面板删除该服务器"
printf '%s\n' "${C_STEP}──────────────────────────────────────────────${C_END}"
