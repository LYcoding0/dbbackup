package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
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

	flag.StringVar(&cfgPath, "config", "config/mysql_backup.json", "Path to config file (JSON)")
	flag.StringVar(&backupTypeOverride, "type", "", "Override backup type: full or incr")
	flag.BoolVar(&skipRemote, "skip-remote", false, "Skip sending to remote storage even if enabled")
	flag.Parse()

	cfg, err := loadConfig(cfgPath)
	if err != nil {
		fatalf("load config: %v", err)
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

	textLines := []string{
		cfg.Feishu.Keyword,
		fmt.Sprintf("状态: %s", status),
		fmt.Sprintf("备份名: %s", backupName),
		fmt.Sprintf("文件: %s", archive),
		fmt.Sprintf("日志: %s", log),
	}
	if errMsg != "" {
		textLines = append(textLines, "错误: "+errMsg)
	}
	payload := map[string]interface{}{
		"msg_type": "text",
		"content": map[string]string{
			"text": strings.Join(textLines, "\n"),
		},
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", cfg.Feishu.Webhook, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "send feishu failed: %v\n", err)
		return
	}
	_ = resp.Body.Close()
}
