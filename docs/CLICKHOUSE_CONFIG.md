# ck-cluster-backup（ClickHouse 集群备份中控）

本工具用于对 ClickHouse 集群所有节点的 `clickhouse-backup` API（默认 7171，Basic Auth）做两阶段备份控制：

- Phase 1：对所有节点并发执行 `POST /backup/create`
- Phase 2：当 Phase 1 全部成功后，再对所有节点并发执行 `POST /backup/upload`
- 轮询：每个节点每个阶段都会每隔几秒调用 `GET /backup/status`，直到返回 `success` 才认为该阶段完成

## 配置文件

示例：`config/ck_cluster_backup.json`

### clickhouse_backup
- `mode`: `api` 或 `ssh`。`api` 使用 HTTP API；`ssh` 在节点上执行 `clickhouse-backup` 命令（API 关闭时用）。
- `scheme`: `http` 或 `https`
- `port`: API 端口，默认 7171
- `username`/`password`: Basic Auth
- `nodes`: 节点 IP/域名列表
- `request_timeout_seconds`: 单次 HTTP 请求超时
- `poll_interval_seconds`: 轮询间隔
- `poll_timeout_seconds`: 单个节点单个阶段的最大等待时间

#### clickhouse_backup.ssh（仅 mode=ssh 时需要）
- `user`: SSH 用户名（你已配置免密）
- `port`: SSH 端口（默认 22）
- `identity_file`: 可选，指定私钥（对应 ssh 的 `-i`）
- `bin`: 可选，clickhouse-backup 命令名/路径（默认 `clickhouse-backup`）
- `extra_args`: 可选，ssh 额外参数数组，例如 `["-o","StrictHostKeyChecking=no"]`
- `batch_mode`: 是否启用 `-o BatchMode=yes`（默认 true，避免交互式密码提示）

### backup
- `type`: 备份类型，`full` 表示普通完整上传，`incr` 表示增量上传。`incr` 默认自动选择每个节点最新正常 full 远端备份作为 `diff_from_remote` 基线。
- `name_prefix`: 备份名前缀，最终名称为 `<name_prefix>_<type>_<YYYYMMDD_HHMMSS>`，例如 `ck_backup_full_20260722_010000`。
- `disk_path`: 可选，飞书通知中展示该路径所在文件系统的总量、已用、可用、使用率和挂载点。建议填 ClickHouse 备份所在分区，例如 `/data/clickhouse/data`。
- `create.configs`: 是否携带 `configs=true` 调用 create（等价 `.../backup/create?name=xxx&configs=true`）
- `upload.enabled`: 是否执行 Upload 阶段（是否上传到远端仓库/MinIO）。为 `false` 时只做 create，不做 upload。
- `upload.diff_from`: 可选，基于本地已有备份做增量上传（对应 `clickhouse-backup upload --diff-from=<name>`）。
- `upload.diff_from_remote`: 可选，基于远端已有备份做增量上传（对应 `clickhouse-backup upload --diff-from-remote=<name>`）。常用于“周一 full，周二到周日 incr”。也可设置为 `latest-full`，由每个节点自动选择同前缀的最新正常 full 远端备份；设置为 `latest` 或 `auto` 时选择最新正常远端备份，不限定 full。如果远端没有可用基线，则本次自动按普通完整上传执行。
- `upload.resumable`: 可选，传给 upload 的 `resumable=true`/`--resumable`，用于支持可恢复上传（需当前 clickhouse-backup 版本支持）。
- `upload.delete_source`: 可选，上传成功后删除本地备份源（需当前 clickhouse-backup 版本支持）。生产环境建议确认恢复策略后再开启。

说明：`diff_from` 和 `diff_from_remote` 不能同时设置。不开这两个字段时，本工具保持普通完整上传行为。
命令行 `-type full|incr` 会覆盖配置文件里的 `backup.type`。`-type full` 会清空增量基线；`-type incr` 如果没有显式指定 `diff_from` / `diff_from_remote`，会自动按 `diff_from_remote=latest-full` 执行。

### log
- `dir`: 日志目录；为空则只输出到 stdout（便于 crontab）

### feishu
- `enabled`: 是否开启飞书告警
- `webhook`: 飞书机器人 webhook
- `keyword`: 飞书安全关键字（必须出现在消息文本中，否则会被拦截）

## 命令行

### 执行集群备份
```bash
ck-cluster-backup backup -config config/ck_cluster_backup.json
```
简写：
```bash
ck-cluster-backup backup -c config/ck_cluster_backup.json
```

指定备份名（不指定则自动生成）：
```bash
ck-cluster-backup backup -config config/ck_cluster_backup.json -name daily_backup_20260125_120000
```

覆盖节点/端口/账号（临时覆盖配置）：
```bash
ck-cluster-backup backup -config config/ck_cluster_backup.json -nodes 10.0.0.11,10.0.0.12 -port 7171 -user api_user -pass api_password
```

执行完整上传：
```bash
ck-cluster-backup backup -config config/ck_cluster_backup.json -type full
```

执行增量上传（自动选择每个节点最新 full 远端备份作为基线）：
```bash
ck-cluster-backup backup -config config/ck_cluster_backup.json -type incr
```

基于上一份远端备份做增量上传：
```bash
ck-cluster-backup backup -config config/ck_cluster_backup.json -diff-from-remote ck_backup_20260722_175304
```

自动选择每个节点最新 full 远端备份作为增量基线：
```bash
ck-cluster-backup backup -config config/ck_cluster_backup.json -diff-from-remote latest-full
```

自动选择每个节点最新远端备份作为增量基线（可能是上一份 incr）：
```bash
ck-cluster-backup backup -config config/ck_cluster_backup.json -diff-from-remote latest
```

只演练（不实际调用 API）：
```bash
ck-cluster-backup backup -config config/ck_cluster_backup.json -dry-run
```

跳过飞书通知：
```bash
ck-cluster-backup backup -config config/ck_cluster_backup.json -skip-feishu
```

### 查看各节点备份列表
```bash
ck-cluster-backup list -config config/ck_cluster_backup.json
```
简写：
```bash
ck-cluster-backup list -c config/ck_cluster_backup.json
```

## API 兼容说明

不同版本/发行的 `clickhouse-backup` API 路径可能不同。本工具已自动兼容常见两种调用方式：
- Query 风格：`POST /backup/upload?name=<name>`
- Path 风格：`POST /backup/upload/<name>`

`curl -X POST ... http://<ip>:7171/backup/upload/<备份名>` 属于第二种，本工具会自动尝试该风格作为 fallback。

## go.mod 说明

本工具需要解析 JSON 配置，使用标准库即可，无额外依赖。

首次拉起依赖建议执行：
```bash
go mod tidy
```
