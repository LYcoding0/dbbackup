# 数据库备份工具

这是一个用 Go 语言编写的多数据库备份工具，支持 MySQL、PostgreSQL 和 MongoDB 的备份。

## 功能特性

### MySQL 备份
- **XtraBackup 物理备份**：支持 Percona XtraBackup 进行物理备份，支持全量和增量备份
- **mysqldump 逻辑备份**：支持 mysqldump 进行逻辑备份
- **智能备份策略**：周一全量备份，其他天增量备份
- **自动清理**：根据配置自动清理过期备份文件
- **远程传输**：支持通过 SCP 将备份文件传输到远程服务器
- **飞书通知**：集成飞书机器人通知备份状态

### PostgreSQL 备份
- **pgBackRest 物理备份**：支持 pgBackRest 进行专业级 PostgreSQL 备份
- **全量/增量/差异备份**：支持多种备份类型
- **自动检查和清理**：集成 pgBackRest 的 check 和 expire 功能
- **飞书通知**：备份状态通知

### MongoDB 备份
- **mongodump 备份**：支持单个数据库或所有数据库备份
- **灵活认证**：支持指定认证数据库
- **额外选项**：支持传递额外的 mongodump 参数

## 项目结构

```
dbbackup/
├── cmd/
│   ├── mysql_xtrabackup/          # MySQL XtraBackup 备份工具
│   ├── native_tool_backup/        # 原生工具备份（MySQL/PostgreSQL/MongoDB）
│   └── postgresql_pgbackrest/     # PostgreSQL pgBackRest 备份工具
├── config/                        # 配置文件目录
│   ├── mysql_backup.json          # MySQL 备份配置
│   └── postgresql_pgbackrest.json # PostgreSQL pgBackRest 配置
├── script/                        # 自动化脚本
│   ├── mysql_backup_cron.sh       # MySQL 定时备份脚本
│   └── postgresql_backup_cron.sh  # PostgreSQL 定时备份脚本
├── mysql_backup_build.bat         # MySQL 工具构建脚本 (Windows)
├── MYSQL_CONFIG.md                # MySQL 配置文件说明
├── pg_backup_build.bat            # PostgreSQL 工具构建脚本 (Windows)
├── POSTGREA_CONFIG.md             # PgreSQL 配置文件说明
└── README.md                      # 项目说明文档
```

## 构建和使用说明

### 构建可执行程序

#### Windows 平台
```shell
# 构建 MySQL 备份工具
mysql_backup_build.bat

# 构建 PostgreSQL 备份工具
pg_backup_build.bat
```

## 使用方法

### 1. MySQL XtraBackup 备份工具

使用 JSON 配置文件方式进行备份。

#### 配置文件示例 (config/mysql_backup.json)
```json
{
  "backup_type": "incr",
  "backup_dir": "/data/backup/tmp",
  "backup_prefix": "mysql",
  "retention_days": 7,
  "tar_archive": true,
  "log_dir": "/data/backup/tmp",
  "mysql": {
    "defaults_file": "/etc/my.cnf",
    "socket": "/data/mysql/mysql.sock",
    "host": "127.0.0.1",
    "port": 3306,
    "user": "root",
    "password": "yourpassword"
  },
  "xtrabackup": {
    "bin": "/usr/bin/xtrabackup",
    "parallel": 2,
    "compress": true,
    "compress_threads": 2,
    "extra_args": []
  },
  "remote": {
    "enabled": false,
    "user": "backup_user",
    "host": "10.80.1.75",
    "port": 22,
    "dest_dir": "/data/backup"
  },
  "feishu": {
    "enabled": true,
    "webhook": "https://open.feishu.cn/open-apis/bot/v2/hook/xxxxx",
    "keyword": "MySQL备份:"
  }
}
```

#### 命令行使用
```bash
# 查看备份信息
./mysql_backup -c config/mysql_backup.json -info

# 执行全量备份
./mysql_backup -c config/mysql_backup.json -type full

# 执行增量备份
./mysql_backup -c config/mysql_backup.json -type incr

# 跳过远程传输
./mysql_backup -c config/mysql_backup.json -type incr -skip-remote
```

### 2. PostgreSQL pgBackRest 备份工具

使用 JSON 配置文件方式，集成 pgBackRest 功能。

#### 配置文件示例 (config/postgresql_pgbackrest.json)
```json
{
  "backup_type": "incr",
  "retention_days": 7,
  "log_dir": "/var/log/dbbackup/pgbackrest",
  "pgbackrest": {
    "bin": "/usr/bin/pgbackrest",
    "config_file": "/etc/pgbackrest/pgbackrest.conf",
    "stanza": "demo",
    "repo_path": "/backup/pgbackup",
    "extra_args": []
  },
  "feishu": {
    "enabled": true,
    "webhook": "https://open.feishu.cn/open-apis/bot/v2/hook/xxxxx",
    "keyword": "PG备份:"
  }
}
```

#### 命令行使用
```bash
# 查看 pgBackRest 信息
./pg_backup -c config/postgresql_pgbackrest.json -info

# 执行全量备份
./pg_backup -c config/postgresql_pgbackrest.json -type full

# 执行增量备份
./pg_backup -c config/postgresql_pgbackrest.json -type incr

# 执行差异备份
./pg_backup -c config/postgresql_pgbackrest.json -type diff
```

### 3. 原生工具备份 (支持 MySQL/PostgreSQL/MongoDB)

使用命令行参数方式进行备份，适合简单场景。

#### MySQL 备份
```bash
# 使用 mysqldump 备份所有数据库
./dbbackup -t mysql -h localhost -P 3306 -u root -p password -out ./backups

# 使用 mysqldump 备份单个数据库
./dbbackup -t mysql -h localhost -P 3306 -u root -p password -db mydb -mysql-all=false -out ./backups

# 使用 xtrabackup 备份（本地备份）
./dbbackup -t mysql -h localhost -P 3306 -u root -p password -mysql-tool xtrabackup -mysql-datadir /var/lib/mysql -out ./backups
```

#### PostgreSQL 备份
```bash
# 备份单个数据库
./dbbackup -t postgresql -h localhost -P 5432 -u postgres -p password -db mydb -out ./backups

# 备份所有数据库
./dbbackup -t postgresql -h localhost -P 5432 -u postgres -p password --postgres-all -out ./backups
```

#### MongoDB 备份
```bash
# 备份单个数据库
./dbbackup -t mongodb -h localhost -P 27017 -u user -p password -db mydb -out ./backups

# 备份所有数据库
./dbbackup -t mongodb -h localhost -P 27017 -u user -p password --mongo-all -out ./backups

# 指定认证数据库
./dbbackup -t mongodb -h localhost -P 27017 -u user -p password -db mydb -mongo-auth-db admin -out ./backups
```

## 配置说明

### MySQL XtraBackup 配置

| 配置项 | 说明 | 必需 |
|--------|------|------|
| `backup_type` | 备份类型：`full` 或 `incr` | 否（默认 `full`） |
| `backup_dir` | 备份文件存储目录 | 是 |
| `backup_prefix` | 备份文件名前缀 | 否（默认 `mysql`） |
| `retention_days` | 保留天数（自动清理） | 否（默认 0，不清理） |
| `tar_archive` | 是否打包为 tar.gz | 否（默认 `true`） |
| `mysql.defaults_file` | my.cnf 路径 | 是 |
| `mysql.socket` | MySQL socket 路径 | 否 |
| `mysql.host` | MySQL 主机 | 否（默认 `127.0.0.1`） |
| `mysql.port` | MySQL 端口 | 否（默认 `3306`） |
| `mysql.user` | MySQL 用户名 | 是 |
| `mysql.password` | MySQL 密码 | 是 |
| `xtrabackup.bin` | xtrabackup 路径 | 否（自动查找） |
| `xtrabackup.parallel` | 并发线程数 | 否（默认 `2`） |
| `xtrabackup.compress` | 是否压缩 | 否（默认 `false`） |
| `xtrabackup.compress_threads` | 压缩线程数 | 否（默认 `2`） |
| `remote.enabled` | 是否启用远程传输 | 否（默认 `false`） |
| `feishu.enabled` | 是否启用飞书通知 | 否（默认 `false`） |

### PostgreSQL pgBackRest 配置

| 配置项 | 说明 | 必需 |
|--------|------|------|
| `backup_type` | 备份类型：`full`、`diff` 或 `incr` | 否（默认 `incr`） |
| `retention_days` | 日志保留天数 | 否（默认 `0`） |
| `pgbackrest.bin` | pgbackrest 路径 | 否（自动查找） |
| `pgbackrest.config_file` | pgbackrest.conf 路径 | 否 |
| `pgbackrest.stanza` | stanza 名称 | 是 |
| `pgbackrest.repo_path` | repo 路径（仅用于显示） | 否 |
| `feishu.enabled` | 是否启用飞书通知 | 否（默认 `false`） |

### 原生工具备份参数（不推荐使用）

#### 通用参数
- `-t`, `-type`：数据库类型（mysql、postgresql、mongodb）
- `-h`, `-host`：数据库主机地址（默认 localhost）
- `-P`, `-port`：数据库端口
- `-u`, `-user`：数据库用户名
- `-p`, `-pass`：数据库密码
- `-db`：数据库名称
- `-out`：备份输出目录（默认 ./backups）

#### MySQL 特定参数
- `-mysql-tool`：备份工具（mysqldump 或 xtrabackup，默认 mysqldump）
- `-mysql-datadir`：MySQL 数据目录（使用 xtrabackup 时必需）
- `-mysql-all`：是否备份所有数据库（默认 true）

#### PostgreSQL 特定参数
- `-postgres-all`：备份所有数据库（使用 pg_dumpall）

#### MongoDB 特定参数
- `-mongo-all`：备份所有数据库
- `-mongo-auth-db`：MongoDB 认证数据库（默认 admin）
- `-mongo-options`：额外的 mongodump 选项

## 自动化脚本

### MySQL 定时备份脚本

脚本会根据周几自动选择备份类型（周一全量，其他天增量）：

```bash
# 配置脚本路径（script/mysql_backup_cron.sh）
#!/bin/bash
SCRIPT_DIR="/usr/local/xxx"
PROJECT_ROOT="/usr/local/xxx"
BINARY="${PROJECT_ROOT}/mysql_backup"
CONFIG="${PROJECT_ROOT}/config/mysql_backup.json"

# 获取今天是周几（1=周一，7=周日）
DAY_OF_WEEK=$(date +%u)

# 根据周几决定备份类型
if [ "$DAY_OF_WEEK" -eq 1 ]; then
    BACKUP_TYPE="full"
else
    BACKUP_TYPE="incr"
fi

# 执行备份
cd "$PROJECT_ROOT" || exit 1
$BINARY -c "$CONFIG" -type "$BACKUP_TYPE" 2>&1
```

#### 添加到 crontab
```bash
# 每天凌晨 1 点执行
crontab -e
# 添加如下示例
0 1 * * * /path/to/dbbackup/script/mysql_backup_cron.sh >> /var/log/mysql_backup_cron.log 2>&1
```

### PostgreSQL 定时备份脚本

```bash
# 配置脚本路径（script/postgresql_backup_cron.sh）
#!/bin/bash
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
BINARY="${PROJECT_ROOT}/pg_backup"
CONFIG="${PROJECT_ROOT}/config/postgresql_pgbackrest.json"

# 获取今天是周几（1=周一，7=周日）
DAY_OF_WEEK=$(date +%u)

# 根据周几决定备份类型
if [ "$DAY_OF_WEEK" -eq 1 ]; then
    BACKUP_TYPE="full"
else
    BACKUP_TYPE="incr"
fi

# 备份前执行 check
sudo -u pgsql /usr/bin/pgbackrest --stanza=demo --config=/etc/pgbackrest/pgbackrest.conf check

# 执行备份
cd "$PROJECT_ROOT" || exit 1
sudo -u pgsql $BINARY -c "$CONFIG" -type "$BACKUP_TYPE" 2>&1

# 备份成功后执行 expire
sudo -u pgsql /usr/bin/pgbackrest --stanza=demo --config=/etc/pgbackrest/pgbackrest.conf expire
```

## 飞书通知集成

工具集成了飞书机器人通知功能，可以实时推送备份状态。

### 配置飞书通知
```json
{
  "feishu": {
    "enabled": true,
    "webhook": "https://open.feishu.cn/open-apis/bot/v2/hook/xxxxx",
    "keyword": "数据库备份:"
  }
}
```

### 飞书消息格式
- **成功通知**：绿色卡片，包含主机名、IP、备份类型、文件路径等信息
- **失败通知**：红色卡片，包含错误详情
- **关键字校验**：确保消息能通过飞书的安全校验

## 故障排除

### 常见错误及解决方案

1. **xtrabackup 命令未找到**
   ```
   xtrabackup command not found
   ```
   解决方案：安装 Percona XtraBackup

2. **pgbackrest 命令未找到**
   ```
   pgbackrest not found in PATH
   ```
   解决方案：安装 pgBackRest

3. **权限不足**
   ```
   Error 1045: Access denied for user 'user'@'host'
   ```
   解决方案：
   - 检查数据库用户权限

4. **备份文件过大**
   - 启用压缩备份
   - 增加并行度以提高备份速度
   - 定期清理过期备份

## 版本信息

- **MySQL XtraBackup 工具**：支持 Percona XtraBackup 8.0
- **PostgreSQL pgBackRest 工具**：支持 pgBackRest 最新版本
- **Go 语言版本**：支持 Go 1.24+
- **跨平台支持**：Windows、Linux、macOS
