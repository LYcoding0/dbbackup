# PostgreSQL pgBackRest Go 工具配置说明

配置文件路径：`config/postgresql_pgbackrest.json`。下面解释各字段含义和常见取值。

## 顶层
- `backup_type`: 备份类型，`full` / `diff` / `incr`（默认 incr）。
- `retention_days`: 本工具日志保留天数（仅清理 `log_dir` 下的旧日志；pgBackRest 备份保留请在 `pgbackrest.conf` 里配置 retention/expire）。
- `log_dir`: 本工具日志目录。

## pgbackrest
- `bin`: pgbackrest 可执行路径；留空则自动从 `PATH` 查找。
- `config_file`: pgbackrest 配置文件路径（可选）；不填则使用 pgbackrest 默认配置查找逻辑。
- `stanza`: stanza 名称（必填）。
- `repo_path`: repo 仓库路径（可选，仅用于日志/飞书消息显示备份位置；实际 repo 配置在 `pgbackrest.conf` 里）。
- `extra_args`: 额外传给 pgbackrest 的参数数组，例如 `["--log-level-console=info"]`。

## feishu
- `enabled`: 是否发送飞书通知。
- `webhook`: 飞书机器人 Webhook 地址。
- `keyword`: 飞书安全关键字（必须出现在消息文本中）。

## 运行示例
```bash
# 显示 info（等同于执行 pgbackrest info）
go run ./cmd/postgresql_pgbackrest -c config/postgresql_pgbackrest.json -info

# 全量
go run ./cmd/postgresql_pgbackrest -c config/postgresql_pgbackrest.json -type full

# 差异
go run ./cmd/postgresql_pgbackrest -c config/postgresql_pgbackrest.json -type diff

# 增量
go run ./cmd/postgresql_pgbackrest -c config/postgresql_pgbackrest.json -type incr
```

