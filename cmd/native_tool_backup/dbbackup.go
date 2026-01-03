package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// MongoDBConfig MongoDB配置结构
type MongoDBConfig struct {
	Host              string
	Port              string
	Username          string
	Password          string
	Database          string
	AuthDatabase      string // 新增：认证数据库
	Options           string
	AllDatabases      bool   // 新增：是否备份所有数据库
}


// 新增PostgreSQL备份配置结构
type PostgresConfig struct {
	Host        string
	Port        string
	Username    string
	Password    string
	Database    string
	AllDatabases bool // 是否备份所有数据库
}

// 新增MySQL配置结构
type MySQLConfig struct {
	Host        string
	Port        string
	Username    string
	Password    string
	Database    string
	AllDatabases bool   // 是否备份所有数据库
	BackupTool  string // "mysqldump" 或 "xtrabackup"
	Datadir     string // 数据目录（使用xtrabackup时必需）
}

func main() {
	// 定义命令行参数（包含简写形式）
	dbType := flag.String("t", "", "Database type: mysql, postgresql, mongodb (shorthand)")
	flag.String("type", "", "Database type: mysql, postgresql, mongodb")
	
	host := flag.String("h", "localhost", "Database host (shorthand)")
	flag.String("host", "localhost", "Database host")
	
	port := flag.String("P", "", "Database port (shorthand)")
	flag.String("port", "", "Database port")
	
	username := flag.String("u", "", "Database username (shorthand)")
	flag.String("user", "", "Database username")
	
	password := flag.String("p", "", "Database password (shorthand)")
	flag.String("pass", "", "Database password")
	
	database := flag.String("db", "", "Database name")
	outputDir := flag.String("out", "./backups", "Backup output directory")
	mongoOptions := flag.String("mongo-options", "", "Additional MongoDB options")
	mongoAuthDB := flag.String("mongo-auth-db", "", "MongoDB authentication database")
	mongoAllDBs := flag.Bool("mongo-all", false, "MongoDB backup all databases")
	
	// MySQL特定参数
	mysqlBackupTool := flag.String("mysql-tool", "mysqldump", "MySQL backup tool: mysqldump or xtrabackup")
	mysqlDatadir := flag.String("mysql-datadir", "/var/lib/mysql", "MySQL data directory (required for xtrabackup)")
	mysqlAllDBs := flag.Bool("mysql-all", true, "MySQL backup all databases (default true)")
	
	// PostgreSQL特定参数
	postgresAllDatabases := flag.Bool("postgres-all", false, "PostgreSQL backup all databases (pg_dumpall)")
	
	// 解析命令行参数
	flag.Parse()
	
	// 处理参数简写形式
	*dbType = getFlagValue("t", "type", *dbType)
	*host = getFlagValue("h", "host", *host)
	*port = getFlagValue("P", "port", *port)
	*username = getFlagValue("u", "user", *username)
	*password = getFlagValue("p", "pass", *password)
	
	// 检查必需参数
	if *dbType == "" {
		fmt.Println("Error: -t or -type is required")
		flag.Usage()
		os.Exit(1)
	}
	
	if *database == "" && *dbType != "mysql" && !*postgresAllDatabases && !*mongoAllDBs {
		fmt.Println("Error: -db is required")
		flag.Usage()
		os.Exit(1)
	}
	
	if *username == "" {
		fmt.Println("Error: -u or -user is required")
		flag.Usage()
		os.Exit(1)
	}
	
	// 设置默认端口
	if *port == "" {
		switch *dbType {
		case "mysql":
			*port = "3306"
		case "postgresql":
			*port = "5432"
		case "mongodb":
			*port = "27017"
		default:
			fmt.Println("Error: unsupported database type")
			os.Exit(1)
		}
	}
	
	// 创建输出目录
	if err := os.MkdirAll(*outputDir, 0755); err != nil {
		fmt.Printf("Error creating output directory: %v\n", err)
		os.Exit(1)
	}
	
	// 根据数据库类型执行备份
	switch strings.ToLower(*dbType) {
	case "mysql":
		config := &MySQLConfig{
			Host:        *host,
			Port:        *port,
			Username:    *username,
			Password:    *password,
			Database:    *database,
			AllDatabases: *mysqlAllDBs,
			BackupTool:  *mysqlBackupTool,
			Datadir:     *mysqlDatadir,
		}
		err := backupMySQL(config, *outputDir)
		if err != nil {
			fmt.Printf("MySQL backup failed: %v\n", err)
			os.Exit(1)
		}
	case "postgresql":
		config := &PostgresConfig{
			Host:         *host,
			Port:         *port,
			Username:     *username,
			Password:     *password,
			Database:     *database,
			AllDatabases: *postgresAllDatabases,
		}
		err := backupPostgreSQL(config, *outputDir)
		if err != nil {
			fmt.Printf("PostgreSQL backup failed: %v\n", err)
			os.Exit(1)
		}
	case "mongodb":
		config := &MongoDBConfig{
			Host:         *host,
			Port:         *port,
			Username:     *username,
			Password:     *password,
			Database:     *database,
			AuthDatabase: *mongoAuthDB,
			Options:      *mongoOptions,
			AllDatabases: *mongoAllDBs,
		}
		err := backupMongoDB(config, *outputDir)
		if err != nil {
			fmt.Printf("MongoDB backup failed: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Printf("Error: unsupported database type '%s'\n", *dbType)
		flag.Usage()
		os.Exit(1)
	}
}

// getFlagValue 获取参数值，支持简写和完整形式
func getFlagValue(short, long, defaultValue string) string {
	// 检查简写参数
	shortValue := getFlagValueByName(short)
	if shortValue != "" {
		return shortValue
	}
	
	// 检查完整参数
	longValue := getFlagValueByName(long)
	if longValue != "" {
		return longValue
	}
	
	// 返回默认值
	return defaultValue
}

// getFlagValueByName 通过参数名称获取参数值
func getFlagValueByName(name string) string {
	found := false
	value := ""
	
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
			value = f.Value.String()
		}
	})
	
	if found {
		return value
	}
	return ""
}

// backupMySQL 备份MySQL数据库，支持mysqldump和xtrabackup
func backupMySQL(config *MySQLConfig, outputDir string) error {
	fmt.Printf("Starting MySQL backup using %s...\n", config.BackupTool)
	
	switch config.BackupTool {
	case "xtrabackup":
		return backupMySQLWithXtraBackup(config, outputDir)
	case "mysqldump":
		return backupMySQLWithMysqldump(config, outputDir)
	default:
		// 默认使用mysqldump方式
		return backupMySQLWithMysqldump(config, outputDir)
	}
}

// backupMySQLWithXtraBackup 使用XtraBackup备份MySQL
func backupMySQLWithXtraBackup(config *MySQLConfig, outputDir string) error {
	fmt.Println("Starting MySQL backup with XtraBackup...")
	
	// 检查xtrabackup命令是否存在
	_, err := exec.LookPath("xtrabackup")
	if err != nil {
		return fmt.Errorf("xtrabackup command not found. Please install Percona XtraBackup: %v", err)
	}
	
	// 创建备份目录
	backupDir := fmt.Sprintf("%s/xtrabackup_%s", outputDir, time.Now().Format("20060102_150405"))
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return fmt.Errorf("failed to create backup directory: %v", err)
	}
	
	// 构建xtrabackup命令
	cmdArgs := []string{
		"--backup",
		"--datadir=" + config.Datadir,
		"--target-dir=" + backupDir,
		"--host=" + config.Host,
		"--port=" + config.Port,
		"--user=" + config.Username,
		"--password=" + config.Password,
	}
	
	cmd := exec.Command("xtrabackup", cmdArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	
	// 创建不包含密码的日志参数用于显示
	logArgs := make([]string, len(cmdArgs))
	copy(logArgs, cmdArgs)
	for i, arg := range logArgs {
		if strings.HasPrefix(arg, "--password=") {
			logArgs[i] = "--password=***"
		}
	}
	
	fmt.Printf("Executing: xtrabackup with args %v\n", logArgs)
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("xtrabackup failed: %v", err)
	}
	
	fmt.Printf("MySQL backup with XtraBackup completed successfully: %s\n", backupDir)
	return nil
}

// backupMySQLWithMysqldump 使用mysqldump备份MySQL
func backupMySQLWithMysqldump(config *MySQLConfig, outputDir string) error {
	fmt.Println("Starting MySQL backup with mysqldump...")
	
	// 检查mysqldump命令是否存在
	_, err := exec.LookPath("mysqldump")
	if err != nil {
		return fmt.Errorf("mysqldump command not found. Please install MySQL client tools: %v", err)
	}
	
	// 构建mysqldump命令
	filename := fmt.Sprintf("%s/mysql_%s.sql", outputDir, time.Now().Format("20060102_150405"))
	cmdArgs := []string{
		"--host=" + config.Host,
		"--port=" + config.Port,
		"--user=" + config.Username,
		"--password=" + config.Password,
		"--single-transaction",
		"--routines",
		"--triggers",
		"--no-tablespaces", // 添加此参数以避免需要PROCESS权限
	}
	
	// 根据是否备份所有数据库添加相应参数
	if config.AllDatabases {
		cmdArgs = append(cmdArgs, "--all-databases") // 备份所有数据库
	} else {
		cmdArgs = append(cmdArgs, config.Database) // 备份指定数据库
	}
	
	cmd := exec.Command("mysqldump", cmdArgs...)
	outputFile, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create output file: %v", err)
	}
	defer outputFile.Close()
	
	cmd.Stdout = outputFile
	cmd.Stderr = os.Stderr
	
	// 创建不包含密码的日志参数用于显示
	logArgs := make([]string, len(cmdArgs))
	copy(logArgs, cmdArgs)
	for i, arg := range logArgs {
		if strings.HasPrefix(arg, "--password=") {
			logArgs[i] = "--password=***"
		}
	}
	
	fmt.Printf("Executing: mysqldump with args %v\n", logArgs)
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("mysqldump failed: %v", err)
	}
	
	fmt.Printf("MySQL backup with mysqldump completed successfully: %s\n", filename)
	return nil
}

// backupPostgreSQL 备份PostgreSQL数据库
func backupPostgreSQL(config *PostgresConfig, outputDir string) error {
	if config.AllDatabases {
		return backupPostgreSQLAll(config, outputDir)
	} else {
		return backupPostgreSQLSingle(config, outputDir)
	}
}

// backupPostgreSQLAll 使用pg_dumpall备份所有PostgreSQL数据库
func backupPostgreSQLAll(config *PostgresConfig, outputDir string) error {
	fmt.Println("Starting PostgreSQL backup of all databases...")
	
	// 检查pg_dumpall命令是否存在
	_, err := exec.LookPath("pg_dumpall")
	if err != nil {
		return fmt.Errorf("pg_dumpall command not found. Please install PostgreSQL client tools: %v", err)
	}
	
	// 设置环境变量
	env := os.Environ()
	env = append(env, fmt.Sprintf("PGHOST=%s", config.Host))
	env = append(env, fmt.Sprintf("PGPORT=%s", config.Port))
	env = append(env, fmt.Sprintf("PGUSER=%s", config.Username))
	env = append(env, fmt.Sprintf("PGPASSWORD=%s", config.Password))
	
	// 构建pg_dumpall命令
	filename := fmt.Sprintf("%s/postgresql_all_%s.sql", outputDir, time.Now().Format("20060102_150405"))
	cmdArgs := []string{
		"--verbose",
		"--clean",
		"--no-owner",
		"--no-acl",
	}
	
	cmd := exec.Command("pg_dumpall", cmdArgs...)
	cmd.Env = env
	
	outputFile, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create output file: %v", err)
	}
	defer outputFile.Close()
	
	cmd.Stdout = outputFile
	cmd.Stderr = os.Stderr
	
	// 创建不包含密码的日志参数用于显示
	logEnv := make([]string, len(env))
	copy(logEnv, env)
	for i, envVar := range logEnv {
		if strings.HasPrefix(envVar, "PGPASSWORD=") {
			logEnv[i] = "PGPASSWORD=***"
		}
	}
	
	fmt.Printf("Executing: pg_dumpall with args %v and env %v\n", cmdArgs, logEnv)
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("pg_dumpall failed: %v", err)
	}
	
	fmt.Printf("PostgreSQL backup of all databases completed successfully: %s\n", filename)
	return nil
}

// backupPostgreSQLSingle 使用pg_dump备份单个PostgreSQL数据库
func backupPostgreSQLSingle(config *PostgresConfig, outputDir string) error {
	fmt.Printf("Starting PostgreSQL backup of database '%s'...\n", config.Database)
	
	// 检查pg_dump命令是否存在
	_, err := exec.LookPath("pg_dump")
	if err != nil {
		return fmt.Errorf("pg_dump command not found. Please install PostgreSQL client tools: %v", err)
	}
	
	// 设置环境变量
	env := os.Environ()
	env = append(env, fmt.Sprintf("PGHOST=%s", config.Host))
	env = append(env, fmt.Sprintf("PGPORT=%s", config.Port))
	env = append(env, fmt.Sprintf("PGUSER=%s", config.Username))
	env = append(env, fmt.Sprintf("PGPASSWORD=%s", config.Password))
	
	// 构建pg_dump命令
	filename := fmt.Sprintf("%s/postgresql_%s_%s.sql", outputDir, config.Database, time.Now().Format("20060102_150405"))
	cmdArgs := []string{
		"--verbose",
		"--clean",
		"--no-owner",
		"--no-acl",
		config.Database,
	}
	
	cmd := exec.Command("pg_dump", cmdArgs...)
	cmd.Env = env
	
	outputFile, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create output file: %v", err)
	}
	defer outputFile.Close()
	
	cmd.Stdout = outputFile
	cmd.Stderr = os.Stderr
	
	// 创建不包含密码的日志参数用于显示
	logEnv := make([]string, len(env))
	copy(logEnv, env)
	for i, envVar := range logEnv {
		if strings.HasPrefix(envVar, "PGPASSWORD=") {
			logEnv[i] = "PGPASSWORD=***"
		}
	}
	
	fmt.Printf("Executing: pg_dump with args %v and env %v\n", cmdArgs, logEnv)
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("pg_dump failed: %v", err)
	}
	
	fmt.Printf("PostgreSQL backup of database '%s' completed successfully: %s\n", config.Database, filename)
	return nil
}

// backupMongoDB 备份MongoDB数据库
func backupMongoDB(config *MongoDBConfig, outputDir string) error {
	if config.AllDatabases {
		return backupMongoDBAll(config, outputDir)
	} else {
		return backupMongoDBSingle(config, outputDir)
	}
}

// backupMongoDBAll 备份所有MongoDB数据库
func backupMongoDBAll(config *MongoDBConfig, outputDir string) error {
	fmt.Println("Starting MongoDB backup of all databases...")
	
	// 检查mongodump命令是否存在
	_, err := exec.LookPath("mongodump")
	if err != nil {
		return fmt.Errorf("mongodump command not found. Please install MongoDB client tools: %v", err)
	}
	
	// 构建mongodump命令（不指定--db参数以备份所有数据库）
	filename := fmt.Sprintf("%s/mongodb_all_%s", outputDir, time.Now().Format("20060102_150405"))
	cmdArgs := []string{
		"--host=" + config.Host + ":" + config.Port,
		"--username=" + config.Username,
		"--password=" + config.Password,
		"--out=" + filename,
	}
	
	// 添加认证数据库参数（如果没有指定则默认使用admin）
	authDB := config.AuthDatabase
	if authDB == "" {
		authDB = "admin"
	}
	cmdArgs = append(cmdArgs, "--authenticationDatabase="+authDB)
	
	// 添加额外选项
	if config.Options != "" {
		cmdArgs = append(cmdArgs, strings.Split(config.Options, " ")...)
	}
	
	cmd := exec.Command("mongodump", cmdArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	
	// 创建不包含密码的日志参数用于显示
	logArgs := make([]string, len(cmdArgs))
	copy(logArgs, cmdArgs)
	for i, arg := range logArgs {
		if strings.HasPrefix(arg, "--password=") {
			logArgs[i] = "--password=***"
		}
	}
	
	fmt.Printf("Executing: mongodump with args %v\n", logArgs)
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("mongodump failed: %v", err)
	}
	
	fmt.Printf("MongoDB backup of all databases completed successfully: %s\n", filename)
	return nil
}

// backupMongoDBSingle 备份单个MongoDB数据库
func backupMongoDBSingle(config *MongoDBConfig, outputDir string) error {
	fmt.Printf("Starting MongoDB backup of database '%s'...\n", config.Database)
	
	// 检查mongodump命令是否存在
	_, err := exec.LookPath("mongodump")
	if err != nil {
		return fmt.Errorf("mongodump command not found. Please install MongoDB client tools: %v", err)
	}
	
	// 构建mongodump命令
	filename := fmt.Sprintf("%s/mongodb_%s_%s", outputDir, config.Database, time.Now().Format("20060102_150405"))
	cmdArgs := []string{
		"--host=" + config.Host + ":" + config.Port,
		"--username=" + config.Username,
		"--password=" + config.Password,
		"--db=" + config.Database,
		"--out=" + filename,
	}
	
	// 添加认证数据库参数（如果没有指定则默认使用admin）
	authDB := config.AuthDatabase
	if authDB == "" {
		authDB = "admin"
	}
	cmdArgs = append(cmdArgs, "--authenticationDatabase="+authDB)
	
	// 添加额外选项
	if config.Options != "" {
		cmdArgs = append(cmdArgs, strings.Split(config.Options, " ")...)
	}
	
	cmd := exec.Command("mongodump", cmdArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	
	// 创建不包含密码的日志参数用于显示
	logArgs := make([]string, len(cmdArgs))
	copy(logArgs, cmdArgs)
	for i, arg := range logArgs {
		if strings.HasPrefix(arg, "--password=") {
			logArgs[i] = "--password=***"
		}
	}
	
	fmt.Printf("Executing: mongodump with args %v\n", logArgs)
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("mongodump failed: %v", err)
	}
	
	fmt.Printf("MongoDB backup of database '%s' completed successfully: %s\n", config.Database, filename)
	return nil
}