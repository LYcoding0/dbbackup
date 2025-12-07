# MySQL XtraBackup Go 工具配置说明

配置文件路径：`config/mysql_backup.json`。下面解释各字段含义和常见取值。

## 顶层
- `backup_type`: 备份类型，`full` 全量，`incr` 增量（增量需要已有最近一次全量目录作为基线，命名满足 `<prefix>_full_*`）。
- `backup_dir`: 本地备份根目录。备份目录/归档会生成在此目录下。
- `backup_prefix`: 备份命名前缀，实际备份目录名形如 `<prefix>_<type>_<timestamp>`.
- `retention_days`: 历史保留天数，超期会清理；`0` 表示不清理。
- `tar_archive`: `true` 则完成后将备份目录打成 `.tar.gz`（上传也用归档）；`false` 则保留目录。
- `log_dir`: 可选，日志目录；为空则默认 `<backup_dir>/log`。

## mysql
- `defaults_file`: MySQL 配置文件路径（包含 socket、数据目录等）。必填。
- `socket`: MySQL socket 路径，若填写则优先使用 socket 连接。
- `host` / `port`: 当未指定 socket 时使用的主机和端口。
- `user` / `password`: 具备备份所需最小权限的账号。

## xtrabackup
- `bin`: xtrabackup 可执行路径；留空则自动从 `PATH` 查找。
- `parallel`: 备份并行度（默认 2）。
- `compress`: 是否启用压缩。
- `compress_threads`: 压缩线程数（`compress` 为 true 时有效）。
- `extra_args`: 额外传给 xtrabackup 的参数数组，例如 `["--throttle=100"]`。

## remote
- `enabled`: 是否开启远端发送（scp）。
- `user` / `host` / `port`: 远端登录信息（端口默认 22）。
- `dest_dir`: 远端存储目录。

## feishu
- `enabled`: 是否发送飞书通知。
- `webhook`: 飞书机器人 Webhook 地址。
- `keyword`: 飞书安全关键字（必须出现在消息文本中）。

## 运行示例
```bash
# 全量
go run ./cmd/mysql_xtrabackup -config config/mysql_backup.json -type full

# 增量（需已有一次 full 基线）
go run ./cmd/mysql_xtrabackup -config config/mysql_backup.json -type incr

# 跳过远端发送（即使 enabled=true 也不上传）
go run ./cmd/mysql_xtrabackup -config config/mysql_backup.json -skip-remote
```
