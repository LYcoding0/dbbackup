#!/bin/bash
########################################
# ClickHouse 集群备份 cron 示例脚本
# 用法：crontab -e 添加，例如：
#   5 2 * * * /usr/local/ck-backup/clickhouse_backup_cron.sh >> /usr/local/ck-backup/log/cron.log 2>&1
# by author ly
########################################

set -e

PROJECT_ROOT="/usr/local/ck-backup"
BINARY="${PROJECT_ROOT}/ck_cluster_backup"
CONFIG="${PROJECT_ROOT}/config/ck_cluster_backup.json"

echo "[$(date '+%Y-%m-%d %H:%M:%S')] starting ck-cluster-backup..."
cd "$PROJECT_ROOT" || exit 1

# 默认执行 backup；也可显式写：$BINARY backup -c "$CONFIG"
$BINARY -c "$CONFIG" 2>&1

EXIT_CODE=$?
if [ $EXIT_CODE -eq 0 ]; then
  echo "[$(date '+%Y-%m-%d %H:%M:%S')] ck-cluster-backup completed successfully"
else
  echo "[$(date '+%Y-%m-%d %H:%M:%S')] ck-cluster-backup failed with exit code $EXIT_CODE" >&2
fi

exit $EXIT_CODE
