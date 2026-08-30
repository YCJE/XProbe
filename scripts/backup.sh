#!/usr/bin/env bash
# XProbe 数据库备份(设计文档 8.6 v1.3):
# WAL 模式下禁止直接 cp —— 必须用 sqlite3 在线备份 API 保证一致性快照。
set -euo pipefail

DATA_DIR="${1:-/var/lib/xprobe-server}"
BACKUP_DIR="${2:-/var/backups/xprobe-server}"
KEEP_DAYS="${3:-30}"

mkdir -p "$BACKUP_DIR"
OUT="${BACKUP_DIR}/xprobe-$(date +%Y%m%d-%H%M%S).db"
sqlite3 "${DATA_DIR}/xprobe.db" ".backup '${OUT}'"
echo "备份完成: ${OUT}"

# 清理过期备份
find "$BACKUP_DIR" -name 'xprobe-*.db' -mtime "+${KEEP_DAYS}" -delete
echo "已清理 ${KEEP_DAYS} 天前的旧备份"
