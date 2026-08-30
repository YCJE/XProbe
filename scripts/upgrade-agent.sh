#!/usr/bin/env bash
# XProbe Agent 手动升级(设计文档 8.5):
# 停止 → 备份 → 替换 → 重新 setcap → 启动。
# 不动配置与 state.json: install_salt 不变则主机指纹不变, 避免误触发 409。
set -euo pipefail

AGENT_URL="${1:?用法: upgrade-agent.sh <binary-url-or-path>}"
BIN=/usr/local/bin/xprobe-agent

systemctl stop xprobe-agent
cp "$BIN" "${BIN}.bak"
if [[ -f "$AGENT_URL" ]]; then
    cp "$AGENT_URL" "$BIN"
else
    curl -fsSL -o "$BIN" "$AGENT_URL"
    curl -fsSL -o "${BIN}.sha256" "${AGENT_URL}.sha256" && \
        echo "$(cat ${BIN}.sha256)  $BIN" | sha256sum -c -
fi
chmod +x "$BIN"
setcap cap_net_raw+ep "$BIN" 2>/dev/null || echo "setcap 失败, 将降级探测"
systemctl start xprobe-agent
systemctl --no-pager status xprobe-agent | head -5
echo "升级完成。回滚: systemctl stop xprobe-agent && mv ${BIN}.bak $BIN && systemctl start xprobe-agent"
