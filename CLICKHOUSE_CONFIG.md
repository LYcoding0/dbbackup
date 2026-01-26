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
- `name_prefix`: 备份名前缀，最终名称为 `<name_prefix>_<YYYYMMDD_HHMMSS>`
- `create.configs`: 是否携带 `configs=true` 调用 create（等价 `.../backup/create?name=xxx&configs=true`）

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

你遇到的 `curl -X POST ... http://<ip>:7171/backup/upload/<备份名>` 属于第二种，本工具会自动尝试该风格作为 fallback。

## go.mod 说明

本工具需要解析 JSON 配置，使用标准库即可，无额外依赖。

首次拉起依赖建议执行：
```bash
go mod tidy
```
