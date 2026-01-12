#!/bin/bash
########################################
#2026年1月12日09点36分
#MySQL 自动备份脚本（周一全量，其他天增量）
#by author ly
######################s##################

# 使用方法：crontab -e 添加：0 1 * * * /path/to/mysql_backup_cron.sh

set -e

# 配置路径
SCRIPT_DIR="/usr/local/xxx"
PROJECT_ROOT="/usr/local/xxx"
BINARY="${PROJECT_ROOT}/mysql_xtrabackup"
CONFIG="${PROJECT_ROOT}/config/mysql_backup.json"

# 获取今天是周几（1=周一，7=周日）
DAY_OF_WEEK=$(date +%u)

# 根据周几决定备份类型
if [ "$DAY_OF_WEEK" -eq 1 ]; then
    BACKUP_TYPE="full"
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] Monday detected, running FULL backup"
else
    BACKUP_TYPE="incr"
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] Day $DAY_OF_WEEK detected, running INCR backup"
fi

# 执行备份
cd "$PROJECT_ROOT" || exit 1
$BINARY -c "$CONFIG" -type "$BACKUP_TYPE" 2>&1

EXIT_CODE=$?
if [ $EXIT_CODE -eq 0 ]; then
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] Backup completed successfully (type=$BACKUP_TYPE)"
else
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] Backup failed with exit code $EXIT_CODE (type=$BACKUP_TYPE)" >&2
fi

exit $EXIT_CODE
