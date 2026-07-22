#!/bin/bash
########################################
# ClickHouse 集群备份 cron 示例脚本（周一 full，其他天 incr）
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

# 获取今天是周几（1=周一，7=周日）
DAY_OF_WEEK=$(date +%u)

if [ "$DAY_OF_WEEK" -eq 1 ]; then
  BACKUP_TYPE="full"
  echo "[$(date '+%Y-%m-%d %H:%M:%S')] Monday detected, running FULL backup"
else
  BACKUP_TYPE="incr"
  echo "[$(date '+%Y-%m-%d %H:%M:%S')] Day $DAY_OF_WEEK detected, running INCR backup"
fi

# 默认执行 backup；也可显式写：$BINARY backup -c "$CONFIG" -type "$BACKUP_TYPE"
$BINARY -c "$CONFIG" -type "$BACKUP_TYPE" 2>&1

EXIT_CODE=$?
if [ $EXIT_CODE -eq 0 ]; then
  echo "[$(date '+%Y-%m-%d %H:%M:%S')] ck-cluster-backup completed successfully (type=$BACKUP_TYPE)"
else
  echo "[$(date '+%Y-%m-%d %H:%M:%S')] ck-cluster-backup failed with exit code $EXIT_CODE (type=$BACKUP_TYPE)" >&2
fi

exit $EXIT_CODE
