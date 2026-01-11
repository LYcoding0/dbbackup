package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Config 备份工具的 JSON 配置。
type Config struct {
	BackupType    string `json:"backup_type"`    // full 或 incr
	BackupDir     string `json:"backup_dir"`     // 本地备份根目录
	BackupPrefix  string `json:"backup_prefix"`  // 备份命名前缀
	RetentionDays int    `json:"retention_days"` // 保留天数
	TarArchive    bool   `json:"tar_archive"`    // 是否打包为 tar.gz

	LogDir string `json:"log_dir"` // 可选，默认 <BackupDir>/log

	MySQL struct {
		DefaultsFile string `json:"defaults_file"` // my.cnf 路径
		Socket       string `json:"socket"`        // 优先使用 socket
		Host         string `json:"host"`
		Port         int    `json:"port"`
		User         string `json:"user"`
		Password     string `json:"password"`
	} `json:"mysql"`

	XtraBackup struct {
		Bin             string   `json:"bin"`              // xtrabackup 路径，不填则 PATH 查找
		Parallel        int      `json:"parallel"`         // --parallel
		Compress        bool     `json:"compress"`         // --compress
		CompressThreads int      `json:"compress_threads"` // --compress-threads
		ExtraArgs       []string `json:"extra_args"`       // 额外参数
	} `json:"xtrabackup"`

	Remote struct {
		Enabled bool   `json:"enabled"` // 是否上传远端
		User    string `json:"user"`
		Host    string `json:"host"`
		Port    int    `json:"port"`
		DestDir string `json:"dest_dir"`
	} `json:"remote"`

	Feishu struct {
		Enabled bool   `json:"enabled"` // 是否发送飞书通知
		Webhook string `json:"webhook"` // 飞书机器人 webhook
		Keyword string `json:"keyword"` // 飞书安全关键字，需出现在文本
	} `json:"feishu"`
}

type backupResult struct {
	BackupName  string
	TargetDir   string
	ArchivePath string
	LogPath     string
}

func main() {
	var cfgPath string
	var backupTypeOverride string
	var skipRemote bool
	var info bool

	flag.StringVar(&cfgPath, "config", "config/config.json", "Path to config file (JSON)")
	flag.StringVar(&cfgPath, "c", "config/config.json", "Shorthand for -config")
	flag.StringVar(&backupTypeOverride, "type", "", "Override backup type: full or incr")
	flag.BoolVar(&skipRemote, "skip-remote", false, "Skip sending to remote storage even if enabled")
	flag.BoolVar(&info, "info", false, "Show backup info and exit")
	flag.Parse()

	cfg, err := loadConfig(cfgPath)
	if err != nil {
		fatalf("load config: %v", err)
	}

	if info {
		if err := normalizeConfigForInfo(cfg); err != nil {
			fatalf("config invalid: %v", err)
		}
		if err := showInfo(cfg); err != nil {
			fatalf("info failed: %v", err)
		}
		return
	}

	if backupTypeOverride != "" {
		cfg.BackupType = backupTypeOverride
	}
	if cfg.BackupType == "" {
		cfg.BackupType = "full"
	}

	if err := validateConfig(cfg); err != nil {
		fatalf("config invalid: %v", err)
	}

	result, err := runBackup(cfg)
	if err != nil {
		sendFeishu(cfg, result, "失败", err.Error())
		fatalf("backup failed: %v", err)
	}

	if cfg.Remote.Enabled && !skipRemote {
		if err := sendArchive(cfg, result); err != nil {
			sendFeishu(cfg, result, "失败", err.Error())
			fatalf("send to remote failed: %v", err)
		}
	}

	if cfg.RetentionDays > 0 {
		if err := cleanupOld(cfg); err != nil {
			sendFeishu(cfg, result, "失败", err.Error())
			fatalf("cleanup failed: %v", err)
		}
	}

	sendFeishu(cfg, result, "成功", "")
	fmt.Printf("Backup finished. name=%s local=%s archive=%s log=%s\n", result.BackupName, result.TargetDir, result.ArchivePath, result.LogPath)
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// info 模式只需要本地目录信息，不强制要求 xtrabackup/mysql/scp 等依赖
func normalizeConfigForInfo(cfg *Config) error {
	if cfg.BackupDir == "" {
		return errors.New("backup_dir is required")
	}
	if cfg.BackupPrefix == "" {
		cfg.BackupPrefix = "mysql"
	}
	if cfg.LogDir == "" {
		cfg.LogDir = filepath.Join(cfg.BackupDir, "log")
	}
	return nil
}

// 运行前检测配置，环境变量，可执行文件...
func validateConfig(cfg *Config) error {
	if cfg.BackupType != "full" && cfg.BackupType != "incr" {
		return fmt.Errorf("backup_type must be full or incr, got %s", cfg.BackupType)
	}
	if cfg.BackupDir == "" {
		return errors.New("backup_dir is required")
	}
	if cfg.BackupPrefix == "" {
		cfg.BackupPrefix = "mysql"
	}
	if cfg.LogDir == "" {
		cfg.LogDir = filepath.Join(cfg.BackupDir, "log")
	}
	if cfg.MySQL.DefaultsFile == "" {
		return errors.New("mysql.defaults_file is required")
	}
	if cfg.MySQL.User == "" || cfg.MySQL.Password == "" {
		return errors.New("mysql.user and mysql.password are required")
	}
	if cfg.MySQL.Socket == "" {
		if cfg.MySQL.Host == "" {
			cfg.MySQL.Host = "127.0.0.1"
		}
		if cfg.MySQL.Port == 0 {
			cfg.MySQL.Port = 3306
		}
	}
	if cfg.XtraBackup.Bin == "" {
		bin, err := exec.LookPath("xtrabackup")
		if err != nil {
			return fmt.Errorf("xtrabackup not found in PATH: %w", err)
		}
		cfg.XtraBackup.Bin = bin
	}
	if cfg.XtraBackup.Parallel == 0 {
		cfg.XtraBackup.Parallel = 2
	}
	if cfg.XtraBackup.Compress && cfg.XtraBackup.CompressThreads == 0 {
		cfg.XtraBackup.CompressThreads = 2
	}
	if cfg.Remote.Enabled {
		if cfg.Remote.User == "" || cfg.Remote.Host == "" || cfg.Remote.DestDir == "" {
			return errors.New("remote.user, remote.host, remote.dest_dir are required when remote.enabled=true")
		}
		if cfg.Remote.Port == 0 {
			cfg.Remote.Port = 22
		}
		if _, err := exec.LookPath("scp"); err != nil {
			return fmt.Errorf("scp not found in PATH: %w", err)
		}
	}
	if cfg.Feishu.Enabled {
		if cfg.Feishu.Webhook == "" {
			return errors.New("feishu.webhook is required when feishu.enabled=true")
		}
		if cfg.Feishu.Keyword == "" {
			return errors.New("feishu.keyword is required when feishu.enabled=true (需满足飞书关键字校验)")
		}
	}
	return nil
}

type backupInfoItem struct {
	Name        string
	Type        string
	Time        time.Time
	DirPath     string
	ArchivePath string
	LogPath     string
	DirSize     int64
	ArchiveSize int64
	LogSize     int64
	ModTime     time.Time
	Expired     bool
}

func showInfo(cfg *Config) error {
	entries, err := os.ReadDir(cfg.BackupDir)
	if err != nil {
		return fmt.Errorf("read backup_dir: %w", err)
	}

	items := map[string]*backupInfoItem{}
	for _, e := range entries {
		name := e.Name()

		isArchive := strings.HasSuffix(name, ".tar.gz")
		baseName := name
		if isArchive {
			baseName = strings.TrimSuffix(name, ".tar.gz")
		}

		typ, ts, ok := parseBackupName(cfg.BackupPrefix, baseName)
		if !ok {
			continue
		}

		it, exists := items[baseName]
		if !exists {
			it = &backupInfoItem{
				Name:    baseName,
				Type:    typ,
				Time:    ts,
				DirSize: -1,
			}
			items[baseName] = it
		}

		fp := filepath.Join(cfg.BackupDir, name)
		info, err := e.Info()
		if err == nil {
			if info.ModTime().After(it.ModTime) {
				it.ModTime = info.ModTime()
			}
		}

		if e.IsDir() {
			it.DirPath = fp
			if sz, err := dirSize(fp); err == nil {
				it.DirSize = sz
			}
			continue
		}

		if isArchive && info != nil {
			it.ArchivePath = fp
			it.ArchiveSize = info.Size()
		}
	}

	// 补充日志信息
	for _, it := range items {
		logPath := filepath.Join(cfg.LogDir, it.Name+".log")
		if st, err := os.Stat(logPath); err == nil && !st.IsDir() {
			it.LogPath = logPath
			it.LogSize = st.Size()
			if st.ModTime().After(it.ModTime) {
				it.ModTime = st.ModTime()
			}
		}
	}

	// 计算过期（仅展示，不删除）
	var cutoff time.Time
	if cfg.RetentionDays > 0 {
		cutoff = time.Now().AddDate(0, 0, -cfg.RetentionDays)
		for _, it := range items {
			if !it.ModTime.IsZero() && it.ModTime.Before(cutoff) {
				it.Expired = true
			}
		}
	}

	// 排序输出（按时间倒序）
	list := make([]*backupInfoItem, 0, len(items))
	for _, it := range items {
		list = append(list, it)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].Time.After(list[j].Time)
	})

	fullCount := 0
	incrCount := 0
	for _, it := range list {
		if it.Type == "full" {
			fullCount++
		} else if it.Type == "incr" {
			incrCount++
		}
	}

	fmt.Printf("backup info\n")
	fmt.Printf("  backup_dir: %s\n", cfg.BackupDir)
	fmt.Printf("  log_dir:    %s\n", cfg.LogDir)
	fmt.Printf("  prefix:     %s\n", cfg.BackupPrefix)
	fmt.Printf("  retention:  %d days\n", cfg.RetentionDays)
	if !cutoff.IsZero() {
		fmt.Printf("  cutoff:     %s (before this will be eligible for cleanup)\n", cutoff.Format("2006-01-02 15:04:05"))
	}
	fmt.Printf("  backups:    %d (full=%d incr=%d)\n", len(list), fullCount, incrCount)
	fmt.Println()

	if len(list) == 0 {
		fmt.Printf("no backups found under %s (prefix=%s)\n", cfg.BackupDir, cfg.BackupPrefix)
		return nil
	}

	for _, it := range list {
		exp := ""
		if it.Expired {
			exp = "  [EXPIRED]"
		}
		fmt.Printf("%s%s\n", it.Name, exp)
		fmt.Printf("  type:     %s\n", it.Type)
		fmt.Printf("  time:     %s\n", it.Time.Format("2006-01-02 15:04:05"))
		if it.DirPath != "" {
			if it.DirSize >= 0 {
				fmt.Printf("  dir:      %s (%s)\n", it.DirPath, humanBytes(it.DirSize))
			} else {
				fmt.Printf("  dir:      %s\n", it.DirPath)
			}
		}
		if it.ArchivePath != "" {
			fmt.Printf("  archive:  %s (%s)\n", it.ArchivePath, humanBytes(it.ArchiveSize))
		}
		if it.LogPath != "" {
			fmt.Printf("  log:      %s (%s)\n", it.LogPath, humanBytes(it.LogSize))
		} else {
			fmt.Printf("  log:      (missing)\n")
		}
		if !it.ModTime.IsZero() {
			fmt.Printf("  mod_time: %s\n", it.ModTime.Format("2006-01-02 15:04:05"))
		}
		fmt.Println()
	}
	return nil
}

func parseBackupName(prefix, name string) (typ string, ts time.Time, ok bool) {
	if !strings.HasPrefix(name, prefix+"_") {
		return "", time.Time{}, false
	}
	rest := strings.TrimPrefix(name, prefix+"_")
	if strings.HasPrefix(rest, "full_") {
		typ = "full"
		rest = strings.TrimPrefix(rest, "full_")
	} else if strings.HasPrefix(rest, "incr_") {
		typ = "incr"
		rest = strings.TrimPrefix(rest, "incr_")
	} else {
		return "", time.Time{}, false
	}
	t, err := time.Parse("20060102_150405", rest)
	if err != nil {
		return "", time.Time{}, false
	}
	return typ, t, true
}

func dirSize(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	if err != nil {
		return 0, err
	}
	return total, nil
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for n >= div*unit && exp < 6 {
		div *= unit
		exp++
	}
	value := float64(n) / float64(div)
	suffix := []string{"KiB", "MiB", "GiB", "TiB", "PiB", "EiB"}[exp]
	return fmt.Sprintf("%.2f %s", value, suffix)
}

// 运行备份
func runBackup(cfg *Config) (*backupResult, error) {
	if err := os.MkdirAll(cfg.BackupDir, 0755); err != nil {
		return nil, fmt.Errorf("create backup_dir: %w", err)
	}
	if err := os.MkdirAll(cfg.LogDir, 0755); err != nil {
		return nil, fmt.Errorf("create log_dir: %w", err)
	}

	ts := time.Now().Format("20060102_150405")
	backupName := fmt.Sprintf("%s_%s_%s", cfg.BackupPrefix, cfg.BackupType, ts)
	targetDir := filepath.Join(cfg.BackupDir, backupName)
	logPath := filepath.Join(cfg.LogDir, backupName+".log")

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}
	defer logFile.Close()

	logger := io.MultiWriter(os.Stdout, logFile)
	fmt.Fprintf(logger, "[%s] starting backup: %s\n", timeStamp(), backupName)

	args := []string{
		"--defaults-file=" + cfg.MySQL.DefaultsFile,
		"--user=" + cfg.MySQL.User,
		"--password=" + cfg.MySQL.Password,
		"--backup",
		"--target-dir=" + targetDir,
		"--parallel=" + fmt.Sprint(cfg.XtraBackup.Parallel),
		"--ftwrl-wait-timeout=300",
		"--backup-lock-timeout=300",
	}
	if cfg.MySQL.Socket != "" {
		args = append(args, "--socket="+cfg.MySQL.Socket)
	} else {
		args = append(args, "--host="+cfg.MySQL.Host, "--port="+fmt.Sprint(cfg.MySQL.Port))
	}
	if cfg.XtraBackup.Compress {
		args = append(args, "--compress", "--compress-threads="+fmt.Sprint(cfg.XtraBackup.CompressThreads))
	}
	if cfg.BackupType == "incr" {
		baseDir, err := findLatestFull(cfg.BackupDir, cfg.BackupPrefix)
		if err != nil {
			return nil, err
		}
		args = append(args, "--incremental-basedir="+baseDir)
		fmt.Fprintf(logger, "[%s] incremental basedir: %s\n", timeStamp(), baseDir)
	}
	args = append(args, cfg.XtraBackup.ExtraArgs...)

	cmd := exec.Command(cfg.XtraBackup.Bin, args...)
	cmd.Stdout = logger
	cmd.Stderr = logger

	fmt.Fprintf(logger, "[%s] exec: %s %s\n", timeStamp(), cfg.XtraBackup.Bin, strings.Join(maskPassword(args), " "))
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("xtrabackup: %w (see log %s)", err, logPath)
	}

	var archivePath string
	if cfg.TarArchive {
		archivePath, err = tarDir(targetDir, logger)
		if err != nil {
			return nil, err
		}
	} else {
		archivePath = targetDir
	}

	fmt.Fprintf(logger, "[%s] backup finished\n", timeStamp())
	return &backupResult{
		BackupName:  backupName,
		TargetDir:   targetDir,
		ArchivePath: archivePath,
		LogPath:     logPath,
	}, nil
}

// 查找最近的一次全量备份
func findLatestFull(root, prefix string) (string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", fmt.Errorf("read backup_dir: %w", err)
	}
	var fulls []string
	p := prefix + "_full_"
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), p) {
			fulls = append(fulls, e.Name())
		}
	}
	if len(fulls) == 0 {
		return "", errors.New("no full backup found, run a full backup first")
	}
	sort.Strings(fulls)
	return filepath.Join(root, fulls[len(fulls)-1]), nil
}

func tarDir(dir string, logger io.Writer) (string, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return "", fmt.Errorf("stat target dir: %w", err)
	}
	if !info.IsDir() {
		return dir, nil
	}
	if _, err := exec.LookPath("tar"); err != nil {
		return "", fmt.Errorf("tar not found in PATH: %w", err)
	}
	base := filepath.Base(dir)
	parent := filepath.Dir(dir)
	archive := dir + ".tar.gz"
	fmt.Fprintf(logger, "[%s] tar %s -> %s\n", timeStamp(), dir, archive)
	cmd := exec.Command("tar", "-czf", archive, "-C", parent, base)
	cmd.Stdout = logger
	cmd.Stderr = logger
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("tar archive failed: %w", err)
	}
	return archive, nil
}

// 发送tar.gz包到存储服务器
func sendArchive(cfg *Config, res *backupResult) error {
	fmt.Printf("[%s] sending archive to %s@%s:%s ...\n", timeStamp(), cfg.Remote.User, cfg.Remote.Host, cfg.Remote.DestDir)
	args := []string{
		"-P", fmt.Sprint(cfg.Remote.Port),
		res.ArchivePath,
		fmt.Sprintf("%s@%s:%s", cfg.Remote.User, cfg.Remote.Host, cfg.Remote.DestDir),
	}
	cmd := exec.Command("scp", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("scp failed: %w", err)
	}
	return nil
}

// 清理大于retention_days的数据
func cleanupOld(cfg *Config) error {
	entries, err := os.ReadDir(cfg.BackupDir)
	if err != nil {
		return fmt.Errorf("cleanup read dir: %w", err)
	}
	cutoff := time.Now().AddDate(0, 0, -cfg.RetentionDays)
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, cfg.BackupPrefix+"_") {
			continue
		}
		fp := filepath.Join(cfg.BackupDir, name)
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			if err := os.RemoveAll(fp); err != nil {
				return fmt.Errorf("cleanup remove %s: %w", fp, err)
			}
			fmt.Printf("[%s] cleaned old backup %s\n", timeStamp(), fp)
		}
	}
	logEntries, _ := os.ReadDir(cfg.LogDir)
	for _, e := range logEntries {
		fp := filepath.Join(cfg.LogDir, e.Name())
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(fp)
		}
	}
	return nil
}

func maskPassword(args []string) []string {
	out := make([]string, len(args))
	copy(out, args)
	for i, v := range out {
		if strings.HasPrefix(v, "--password=") {
			out[i] = "--password=***"
		}
	}
	return out
}

func timeStamp() string {
	return time.Now().Format("2006-01-02 15:04:05")
}

func fatalf(format string, a ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
	os.Exit(1)
}

// 获取本机 IP（非 127.0.0.1）
func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "unknown"
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return "unknown"
}

// 发送飞书消息
func sendFeishu(cfg *Config, res *backupResult, status string, errMsg string) {
	if !cfg.Feishu.Enabled {
		return
	}

	backupName := ""
	archive := ""
	log := ""
	if res != nil {
		backupName = res.BackupName
		archive = res.ArchivePath
		log = res.LogPath
	}

	// 关键字校验：确保 keyword 出现在 title / content 中
	titlePrefix := ""
	if cfg.Feishu.Keyword != "" {
		titlePrefix = "[" + cfg.Feishu.Keyword + "] "
	}
	title := titlePrefix + "MySQL 备份" + status
	if backupName != "" {
		title += ": " + backupName
	}

	host, _ := os.Hostname()
	ip := getLocalIP()
	color := "orange"
	switch status {
	case "成功":
		color = "green"
	case "失败":
		color = "red"
	}

	mdLines := []string{}
	if cfg.Feishu.Keyword != "" {
		mdLines = append(mdLines, cfg.Feishu.Keyword)
	}
	mdLines = append(mdLines,
		fmt.Sprintf("**状态**：%s", status),
		fmt.Sprintf("**主机**：%s", host),
		fmt.Sprintf("**IP**：%s", ip),
		fmt.Sprintf("**类型**：%s", cfg.BackupType),
		fmt.Sprintf("**备份名**：%s", backupName),
		fmt.Sprintf("**文件**：%s", archive),
		fmt.Sprintf("**日志**：%s", log),
		fmt.Sprintf("**时间**：%s", time.Now().Format("2006-01-02 15:04:05")),
	)
	if errMsg != "" {
		// lark_md 支持反引号，避免错误信息换行/特殊字符破坏布局
		mdLines = append(mdLines, fmt.Sprintf("**错误**：`%s`", strings.ReplaceAll(errMsg, "`", "'")))
	}
	md := strings.Join(mdLines, "\n")

	// 优先发送卡片消息（可读性更好）；若失败则回退到 text，避免通知丢失
	cardPayload := map[string]interface{}{
		"msg_type": "interactive",
		"card": map[string]interface{}{
			"config": map[string]bool{
				"wide_screen_mode": true,
			},
			"header": map[string]interface{}{
				"title": map[string]string{
					"tag":     "plain_text",
					"content": title,
				},
				"template": color,
			},
			"elements": []map[string]interface{}{
				{
					"tag": "div",
					"text": map[string]string{
						"tag":     "lark_md",
						"content": md,
					},
				},
			},
		},
	}
	if err := postFeishuWebhook(cfg.Feishu.Webhook, cardPayload); err == nil {
		return
	}

	textLines := []string{
		cfg.Feishu.Keyword,
		fmt.Sprintf("状态: %s", status),
		fmt.Sprintf("主机: %s", host),
		fmt.Sprintf("类型: %s", cfg.BackupType),
		fmt.Sprintf("备份名: %s", backupName),
		fmt.Sprintf("文件: %s", archive),
		fmt.Sprintf("日志: %s", log),
	}
	if errMsg != "" {
		textLines = append(textLines, "错误: "+errMsg)
	}
	textPayload := map[string]interface{}{
		"msg_type": "text",
		"content": map[string]string{
			"text": strings.Join(textLines, "\n"),
		},
	}
	if err := postFeishuWebhook(cfg.Feishu.Webhook, textPayload); err != nil {
		fmt.Fprintf(os.Stderr, "send feishu failed: %v\n", err)
	}
}

func postFeishuWebhook(webhook string, payload map[string]interface{}) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", webhook, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}
