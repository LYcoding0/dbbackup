#!/bin/bash
########################################
#2026年1月12日09点36分
#PostgreSQL 自动备份脚本（周一全量，其他天增量）
#by author ly
######################s##################

# 使用方法：crontab -e 添加：0 1 * * * /path/to/postgresql_backup_cron.sh

set -e

# 配置路径（根据你的实际部署位置修改）
SCRIPT_DIR="/usr/local/xxx"
PROJECT_ROOT="/usr/local/xxx"
BINARY="${PROJECT_ROOT}/postgresql_pgbackrest" # 或你的实际二进制路径
CONFIG="${PROJECT_ROOT}/config/postgresql_pgbackrest.json"

# pgBackRest 配置（用于 check 和 expire）
STANZA="demo"
PGBACKREST_CONFIG="/etc/pgbackrest/pgbackrest.conf"  # pgbackrest.conf 路径
PGBACKREST_BIN="/usr/bin/pgbackrest"  # pgbackrest 可执行文件路径

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

# 备份前执行 check（确保归档链路正常）
echo "[$(date '+%Y-%m-%d %H:%M:%S')] Running pgbackrest check before backup..."
cd "$PROJECT_ROOT" || exit 1
if sudo -u pgsql $PGBACKREST_BIN --stanza="$STANZA" --config="$PGBACKREST_CONFIG" check 2>&1; then
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] Check passed, proceeding with backup"
else
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] Check FAILED, aborting backup" >&2
    exit 1
fi

# 执行备份（使用 pgsql 用户，避免权限问题）
sudo -u pgsql $BINARY -c "$CONFIG" -type "$BACKUP_TYPE" 2>&1

EXIT_CODE=$?
if [ $EXIT_CODE -eq 0 ]; then
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] Backup completed successfully (type=$BACKUP_TYPE)"
    # 备份成功后，执行 expire 清理过期备份
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] Running pgbackrest expire to clean old backups..."
    if sudo -u pgsql $PGBACKREST_BIN --stanza="$STANZA" --config="$PGBACKREST_CONFIG" expire 2>&1; then
        echo "[$(date '+%Y-%m-%d %H:%M:%S')] Expire completed"
    else
        echo "[$(date '+%Y-%m-%d %H:%M:%S')] Expire failed" >&2
    fi
else
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] Backup failed with exit code $EXIT_CODE (type=$BACKUP_TYPE)" >&2
fi

exit $EXIT_CODE
