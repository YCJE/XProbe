#!/usr/bin/env bash
# S4 审计门禁(CI, 设计文档 7.2/7.8): Agent 代码中命令执行相关符号必须零匹配。
# 任一命中即发布失败——把"纯只读架构"从人工审计变为每次构建可验证。
set -euo pipefail
cd "$(dirname "$0")/.."

echo "[audit-noexec] scanning agent/ for forbidden exec symbols..."
if grep -rnE '"os/exec"|exec\.Command|syscall\.Exec|\bpty\b|\bshell\b|StartProcess' \
    agent/ --include='*.go'; then
    echo "[audit-noexec] FAIL: forbidden exec symbols found in agent/ (violates S4)"
    exit 1
fi
echo "[audit-noexec] OK: agent/ contains zero exec symbols (S4)"
