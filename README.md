# 数据库备份工具

这是一个用 Go 语言编写的多数据库备份工具，支持 MySQL、PostgreSQL 和 MongoDB。

## 功能特性

1. 支持 MySQL 8.0 备份（使用 mysqldump 备份单个或所有数据库，或使用 xtrabackup）
2. 支持 PostgreSQL 备份（使用 pg_dump 或 pg_dumpall）
3. 支持 MongoDB 备份（使用 mongodump 备份单个或所有数据库）

## 构建和使用说明

### 构建可执行程序

您可以使用 `go build` 命令将 Go 源代码编译成可执行程序：

#### Windows 平台
```cmd
go build -o dbbackup.exe dbbackup.go
```

#### Linux/macOS 平台
```bash
go build -o dbbackup dbbackup.go
```

#### 跨平台构建

您还可以使用 Go 的交叉编译功能为不同平台构建可执行文件：

```bash
# 构建 Linux 版本
GOOS=linux GOARCH=amd64 go build -o dbbackup-linux dbbackup.go

# 构建 Windows 版本
GOOS=windows GOARCH=amd64 go build -o dbbackup-windows.exe dbbackup.go

# 构建 macOS 版本
GOOS=darwin GOARCH=amd64 go build -o dbbackup-macos dbbackup.go
```

### 使用可执行程序

构建完成后，您可以直接运行生成的可执行文件，而无需每次都使用 `go run`：

```cmd
# Windows
.\dbbackup.exe -type mysql -host 10.80.0.xx -user root -pass yourpassword

# Linux/macOS
./dbbackup -type mysql -host 10.80.0.xx -user root -pass yourpassword
```

### 优势

1. **性能更好**：直接运行可执行文件比使用 `go run` 更快，因为不需要在运行时编译代码
2. **部署简单**：只需要一个可执行文件，无需 Go 开发环境
3. **便于分发**：可以将可执行文件分发给其他人使用，无需提供源代码
4. **生产环境友好**：更适合在生产环境中使用

## 使用方法

### MySQL 备份

```bash
# 使用默认的 mysqldump 备份所有数据库（简写参数）
./dbbackup -t mysql -h localhost -P 3306 -u root -p yourpassword -out ./backups

# 使用 mysqldump 备份所有数据库（完整参数）
./dbbackup -type mysql -host localhost -port 3306 -user root -pass yourpassword -out ./backups

# 使用 mysqldump 备份单个数据库
./dbbackup -type mysql -host localhost -port 3306 -user root -pass yourpassword -db yourdatabase -mysql-all=false -out ./backups

# 使用 xtrabackup 备份（只能备份所有数据库）
./dbbackup -type mysql -host localhost -port 3306 -user root -pass yourpassword -mysql-tool xtrabackup -mysql-datadir /var/lib/mysql -out ./backups
```

### PostgreSQL 备份

```bash
# 备份单个数据库（简写参数）
./dbbackup -t postgresql -h localhost -P 5432 -u postgres -p yourpassword -db yourdatabase -out ./backups

# 备份所有数据库（简写参数）
./dbbackup -t postgresql -h localhost -P 5432 -u postgres -p yourpassword --postgres-all -out ./backups

# 使用完整参数备份单个数据库
./dbbackup -type postgresql -host localhost -port 5432 -user postgres -pass yourpassword -db yourdatabase -out ./backups

# 使用完整参数备份所有数据库
./dbbackup -type postgresql -host localhost -port 5432 -user postgres -pass yourpassword -postgres-all -out ./backups
```

### MongoDB 备份

```bash
# 备份单个数据库（简写参数）
./dbbackup -t mongodb -h localhost -P 27017 -u youruser -p yourpassword -db yourdatabase -out ./backups

# 备份所有数据库（简写参数）
./dbbackup -t mongodb -h localhost -P 27017 -u youruser -p yourpassword --mongo-all -out ./backups

# 使用完整参数备份单个数据库
./dbbackup -type mongodb -host localhost -port 27017 -user youruser -pass yourpassword -db yourdatabase -out ./backups

# 使用完整参数备份所有数据库
./dbbackup -type mongodb -host localhost -port 27017 -user youruser -pass yourpassword -mongo-all -out ./backups

# 备份单个数据库并指定认证数据库
./dbbackup -type mongodb -host localhost -port 27017 -user youruser -pass yourpassword -db yourdatabase -mongo-auth-db admin -out ./backups
```

## 命令行参数

### 通用参数
- `-t`, `-type`：数据库类型（mysql、postgresql、mongodb）
- `-h`, `-host`：数据库主机地址（默认 localhost）
- `-P`, `-port`：数据库端口
- `-u`, `-user`：数据库用户名
- `-p`, `-pass`：数据库密码
- `-db`：数据库名称（PostgreSQL 和 MongoDB 备份单个数据库时必需，MySQL 备份单个数据库时必需）
- `-out`：备份输出目录（默认 ./backups）

### MySQL 特定参数
- `-mysql-tool`：MySQL 备份工具（mysqldump 或 xtrabackup，默认 mysqldump）
- `-mysql-datadir`：MySQL 数据目录（使用 xtrabackup 时必需）
- `-mysql-all`：是否备份所有 MySQL 数据库（默认 true）

### PostgreSQL 特定参数
- `-postgres-all`：备份所有 PostgreSQL 数据库（使用 pg_dumpall）

### MongoDB 特定参数
- `-mongo-all`：备份所有 MongoDB 数据库
- `-mongo-auth-db`：MongoDB 认证数据库（通常为 admin）
- `-mongo-options`：MongoDB 的额外选项

## 数据库备份数据流向说明

### 命令执行环境
- **执行位置**：您运行命令的服务器（我们称之为"备份服务器"）
- **目标数据库**：位于指定主机地址的数据库（我们称之为"数据库服务器"）

### 备份数据存储位置

#### 使用 xtrabackup 时：
```
备份服务器 (执行命令的机器) → 连接到 → 数据库服务器
                                ↓
备份文件存储在 → 备份服务器的指定输出目录中
```

#### 使用 mysqldump 时：
```
备份服务器 (执行命令的机器) → 连接到 → 数据库服务器
                                ↓
备份文件存储在 → 备份服务器的指定输出目录中
```

### 重要区别说明

#### xtrabackup 的特殊要求
- **xtrabackup 只能进行本地备份**，不能进行远程备份
- 这意味着 `-host` 参数对于 xtrabackup 实际上是无效的
- 如果要使用 xtrabackup，必须在数据库服务器本机上运行备份命令

#### mysqldump 的灵活性
- **mysqldump 支持远程备份**
- 可以在任何能够连接到数据库的机器上运行
- 备份文件会存储在执行命令的机器上

## XtraBackup 工作原理详解

### 什么是 Percona XtraBackup

Percona XtraBackup 是一个开源的 MySQL 数据库热备份工具，由 Percona 公司开发。它能够对 InnoDB 和 XtraDB 数据库引擎进行非阻塞备份，同时支持 MyISAM 等其他存储引擎（需要短暂锁表）。

### XtraBackup 备份原理

#### 1. Redo Log 扫描
XtraBackup 的核心机制是基于 InnoDB 存储引擎的特性：
- InnoDB 使用重做日志（Redo Log）来确保事务的持久性
- XtraBackup 会扫描重做日志以获取备份过程中发生的数据变更

#### 2. 物理备份过程
XtraBackup 执行备份分为两个阶段：

##### 阶段1：数据文件复制（备份阶段）
1. 启动备份过程，开始复制 InnoDB 数据文件（.ibd 文件）
2. 同时监控和记录重做日志的变化
3. 复制其他 MySQL 文件（如表结构文件 .frm）
4. 复制 MyISAM 表（如果存在，需要短暂锁表）

##### 阶段2：日志应用（准备阶段）
1. 使用记录的重做日志来应用备份过程中发生的数据变更
2. 确保备份数据的一致性
3. 生成一致性的备份集

### XtraBackup 的优势

#### 1. 热备份能力
- 备份过程中数据库可以继续处理请求
- 不会阻塞正常的数据库操作

#### 2. 快速备份和恢复
- 物理备份比逻辑备份（如 mysqldump）更快
- 恢复时只需复制文件，速度远超 SQL 导入

#### 3. 压缩和流式传输
- 支持备份压缩以节省存储空间
- 支持直接流式传输到其他系统

#### 4. 增量备份
- 支持基于 LSN（日志序列号）的增量备份
- 可以大大减少备份数据量

### 与 mysqldump 的比较

| 特性 | XtraBackup | mysqldump |
|------|------------|-----------|
| 备份类型 | 物理备份 | 逻辑备份 |
| 速度 | 快（尤其是大数据量） | 慢 |
| 备份期间数据库可用性 | 可用 | 可用 |
| 恢复速度 | 快 | 慢 |
| 跨版本兼容性 | 有限制 | 良好 |
| 存储引擎支持 | 主要支持 InnoDB | 支持所有存储引擎 |
| 增量备份 | 支持 | 不支持 |
| 压缩 | 支持 | 支持 |

### 适用场景

#### XtraBackup 适用于：
1. 大型数据库（GB 或 TB 级别）
2. 需要快速备份和恢复的场景
3. 可以安装额外工具的环境
4. 对备份窗口时间敏感的应用

#### mysqldump 适用于：
1. 小型到中型数据库
2. 需要跨版本迁移的场景
3. 不能安装额外工具的环境
4. 需要查看和编辑备份内容的场景

## 兼容性

- MySQL 8.0 与 Percona XtraBackup 8.0 兼容
- 支持 MySQL 8.0.41 和 XtraBackup 8.0.35-33

## 注意事项

1. 需要安装相应的数据库客户端工具：
   - MySQL: mysqldump 或 xtrabackup
   - PostgreSQL: pg_dump 和 pg_dumpall
   - MongoDB: mongodump

2. 使用 xtrabackup 时需要具有相应数据目录的读取权限

3. 对于 MongoDB，如果用户是在 admin 数据库中创建的，需要使用 `-mongo-auth-db admin` 参数

4. 参数简写形式和完整形式可以混合使用

5. **避免在命令行中暴露密码**，推荐使用环境变量：
   ```bash
   export DB_PASSWORD=yourpassword
   ./dbbackup -type mysql -host 10.80.0.xx -user root -pass $DB_PASSWORD
   ```

6. **使用专用备份用户**以提高安全性：
   ```sql
   -- 创建专用的备份用户（MySQL）
   CREATE USER 'backup_user'@'%' IDENTIFIED BY 'strong_password';
   GRANT SELECT, LOCK TABLES, SHOW VIEW, EVENT, TRIGGER ON *.* TO 'backup_user'@'%';
   FLUSH PRIVILEGES;
   ```

## 故障排除

### 常见错误及解决方案

1. **连接被拒绝**
   ```
   Error: dial tcp 10.80.0.xx:3306: connectex: No connection could be made because the target machine actively refused it.
   ```
   解决方案：
   - 检查数据库服务是否运行
   - 检查防火墙设置
   - 确认数据库绑定地址

2. **权限不足**
   ```
   Error 1045: Access denied for user 'user'@'host'
   ```
   解决方案：
   - 检查数据库用户权限
   - 确认用户可以从您的IP连接

3. **xtrabackup相关错误**
   ```
   xtrabackup: Error: Failed to connect to MySQL server
   ```
   解决方案：
   - xtrabackup只能用于本地备份，不能用于远程备份
   - 远程MySQL备份请使用mysqldump

## 最佳实践建议

### 网络执行注意事项

1. **网络连接**
   确保您的本地机器可以访问远程数据库服务器：
   ```bash
   # 测试网络连接
   ping 10.80.0.xx

   # 测试端口连通性
   telnet 10.80.0.xx 3306
   # 或者使用nc
   nc -zv 10.80.0.xx 3306
   ```

2. **数据库访问权限**
   确保数据库用户具有远程访问权限：
   ```sql
   -- MySQL示例：允许root用户从任何主机连接
   GRANT ALL PRIVILEGES ON *.* TO 'root'@'%' IDENTIFIED BY 'yourpassword';

   -- 或者只允许从特定IP连接
   GRANT ALL PRIVILEGES ON *.* TO 'root'@'your-local-ip' IDENTIFIED BY 'yourpassword';

   -- 刷新权限
   FLUSH PRIVILEGES;
   ```

3. **防火墙设置**
   确保远程服务器防火墙允许数据库端口的入站连接：
   ```bash
   # 在远程服务器上检查防火墙设置（以iptables为例）
   iptables -L -n | grep 3306

   # 在云服务器上，还需要检查安全组规则
   ```

### 性能优化建议

1. **压缩传输**
   对于大数据量的备份，考虑在网络传输时进行压缩：
   ```bash
   # 使用gzip压缩备份输出
   ./dbbackup -type mysql -host 10.80.0.xx -user root -pass yourpassword | gzip > backup.sql.gz
   ```

2. **限流**
   对于生产环境，避免备份操作影响正常业务：
   ```bash
   # 使用nice调整进程优先级
   nice -n 19 ./dbbackup -type mysql -host 10.80.0.xx -user root -pass yourpassword

   # 使用ionice调整IO优先级
   ionice -c 3 ./dbbackup -type mysql -host 10.80.0.xx -user root -pass yourpassword
   ```

## 自动化脚本示例

创建一个批处理脚本自动化备份过程：
```batch
@echo off
setlocal

set BACKUP_HOST=10.80.0.xx
set BACKUP_USER=root
set BACKUP_PASS=yourpassword
set BACKUP_DIR=./backups

echo Starting MySQL backup...
./dbbackup -type mysql -host %BACKUP_HOST% -user %BACKUP_USER% -pass %BACKUP_PASS% -out %BACKUP_DIR%

if %ERRORLEVEL% EQU 0 (
    echo Backup completed successfully
) else (
    echo Backup failed with error level %ERRORLEVEL%
)

pause
```