# 数据库备份工具集合（dbbackup）

一个用 Go 编写的备份工具集合，覆盖：
- MySQL：基于 Percona XtraBackup 的物理备份（全量/增量），可选打包与 SCP 上传，支持飞书通知
- PostgreSQL：基于 pgBackRest 的备份（full/diff/incr），支持 `info` 查看与飞书通知
- ClickHouse 集群：基于 clickhouse-backup 的集群中控（并发两阶段 create/upload + 轮询），支持 API/SSH 两种模式与飞书通知

## 项目结构

```
dbbackup/
  cmd/
    mysql_xtrabackup/          # MySQL XtraBackup 备份工具
    postgresql_pgbackrest/     # PostgreSQL pgBackRest 备份工具
    ck_cluster_backup/         # ClickHouse 集群备份中控（ck-cluster-backup）
    native_tool_backup/        # 旧的通用备份工具（不推荐）
  config/
    mysql_backup.json
    postgresql_pgbackrest.json
    ck_cluster_backup.json
  docs/
    MYSQL_CONFIG.md
    POSTGRES_CONFIG.md
    CLICKHOUSE_CONFIG.md
  script/
    mysql_backup_cron.sh
    postgresql_backup_cron.sh
    clickhouse_backup_cron.sh
  build/
    mysql_backup_build.bat
    pg_backup_build.bat
    ck_backup_build.bat
  dist/                        # Windows 打包脚本输出目录
  README.md
```

## 构建（Windows → Linux amd64）

这几个 bat 会交叉编译出 Linux 可执行文件到 `dist/`：
- MySQL：`build/mysql_backup_build.bat` → `dist/mysql_backup`
- PostgreSQL：`build/pg_backup_build.bat` → `dist/pg_backup`
- ClickHouse：`build/ck_backup_build.bat` → `dist/ck_cluster_backup`

说明：
- `-buildvcs=false`：禁用 VCS 信息写入，避免 “error obtaining VCS status”
- `-trimpath`：去掉本机路径信息，让产物更干净

## 使用：MySQL（XtraBackup）

- 配置：`config/mysql_backup.json`
- 配置说明：`docs/MYSQL_CONFIG.md`

常用命令：
```bash
# 全量
./mysql_backup -c config/mysql_backup.json -type full

# 增量（基于 backup_dir 下最新的 <prefix>_full_* 目录）
./mysql_backup -c config/mysql_backup.json -type incr

# 跳过远端上传
./mysql_backup -c config/mysql_backup.json -type incr -skip-remote
```

## 使用：PostgreSQL（pgBackRest）

- 配置：`config/postgresql_pgbackrest.json`
- 配置说明：`docs/POSTGRES_CONFIG.md`

常用命令：
```bash
# 查看 pgBackRest 信息
./pg_backup -c config/postgresql_pgbackrest.json -info

# 执行备份（full/diff/incr）
./pg_backup -c config/postgresql_pgbackrest.json -type incr
```

## 使用：ClickHouse 集群（clickhouse-backup 中控）

- 配置：`config/ck_cluster_backup.json`
- 配置说明：`docs/CLICKHOUSE_CONFIG.md`

### 备份（并发两阶段 + 轮询）
```bash
# 完整上传（默认 backup 子命令可省略）
./ck_cluster_backup -c config/ck_cluster_backup.json -type full

# 增量上传（自动选择每个节点最近一次 full 远端备份作为基线）
./ck_cluster_backup -c config/ck_cluster_backup.json -type incr

# 显式写法
./ck_cluster_backup backup -c config/ck_cluster_backup.json -type incr
```

提示：如果你只想在本地生成备份、不上传 MinIO/远端仓库，可在 `config/ck_cluster_backup.json` 中设置 `backup.upload.enabled=false`。

### 查看各节点备份列表
```bash
./ck_cluster_backup list -c config/ck_cluster_backup.json
```

### API / SSH 模式

在 `config/ck_cluster_backup.json` 中用 `clickhouse_backup.mode` 选择：
- `api`：通过 `http://<node>:7171` 调用 API（Basic Auth），并轮询 `GET /backup/status` 等待 `success`
- `ssh`：当 API 关闭时，通过 SSH 在每个节点执行 `clickhouse-backup create/upload/list`

### upload 接口兼容

不同版本的 clickhouse-backup API 可能有两种风格：
- Query：`POST /backup/upload?name=<name>`
- Path：`POST /backup/upload/<name>`

工具会自动 fallback 兼容两种路径。

## 自动化（cron）

脚本目录 `script/` 提供示例：
- `script/mysql_backup_cron.sh`
- `script/postgresql_backup_cron.sh`
- `script/clickhouse_backup_cron.sh`
