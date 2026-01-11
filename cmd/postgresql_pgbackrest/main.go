package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Config pgBackRest 备份工具的 JSON 配置。
type Config struct {
	BackupType    string `json:"backup_type"`    // full / diff / incr
	RetentionDays int    `json:"retention_days"` // 清理本工具日志保留天数（pgbackrest 备份保留请用 pgbackrest.conf 管理）
	LogDir        string `json:"log_dir"`        // 本工具日志目录

	PgBackRest struct {
		Bin        string   `json:"bin"`         // pgbackrest 路径；不填则 PATH 查找
		ConfigFile string   `json:"config_file"` // pgbackrest.conf 路径（可选）
		Stanza     string   `json:"stanza"`      // stanza 名称（必填）
		RepoPath   string   `json:"repo_path"`   // repo 路径（可选，仅用于显示）
		ExtraArgs  []string `json:"extra_args"`  // 额外参数
	} `json:"pgbackrest"`

	Feishu struct {
		Enabled bool   `json:"enabled"`
		Webhook string `json:"webhook"`
		Keyword string `json:"keyword"`
	} `json:"feishu"`
}

type backupResult struct {
	Stanza      string
	BackupType  string
	BackupLabel string
	RepoPath    string
	LogPath     string
}

func main() {
	var cfgPath string
	var backupTypeOverride string
	var info bool

	flag.StringVar(&cfgPath, "config", "config/config.json", "Path to config file (JSON)")
	flag.StringVar(&cfgPath, "c", "config/config.json", "Shorthand for -config")
	flag.StringVar(&backupTypeOverride, "type", "", "Override backup type: full, diff, incr")
	flag.BoolVar(&info, "info", false, "Show pgBackRest info and exit")
	flag.Parse()

	cfg, err := loadConfig(cfgPath)
	if err != nil {
		fatalf("load config: %v", err)
	}

	if backupTypeOverride != "" {
		cfg.BackupType = backupTypeOverride
	}
	if cfg.BackupType == "" {
		cfg.BackupType = "incr"
	}

	if err := validateConfig(cfg); err != nil {
		fatalf("config invalid: %v", err)
	}

	if info {
		if err := showInfo(cfg); err != nil {
			fatalf("info failed: %v", err)
		}
		return
	}

	res, err := runBackup(cfg)
	if err != nil {
		sendFeishu(cfg, res, "失败", err.Error())
		fatalf("backup failed: %v", err)
	}

	if cfg.RetentionDays > 0 {
		_ = cleanupOldLogs(cfg)
	}

	sendFeishu(cfg, res, "成功", "")
	fmt.Printf("Backup finished. stanza=%s type=%s label=%s repo=%s log=%s\n",
		res.Stanza, res.BackupType, res.BackupLabel, res.RepoPath, res.LogPath)
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
	switch cfg.BackupType {
	case "full", "diff", "incr":
	default:
		return fmt.Errorf("backup_type must be full/diff/incr, got %s", cfg.BackupType)
	}
	if cfg.PgBackRest.Stanza == "" {
		return errors.New("pgbackrest.stanza is required")
	}
	if cfg.LogDir == "" {
		// 默认放到当前目录下，避免必须写绝对路径
		cfg.LogDir = filepath.Join(".", "log")
	}
	if cfg.PgBackRest.Bin == "" {
		bin, err := exec.LookPath("pgbackrest")
		if err != nil {
			return fmt.Errorf("pgbackrest not found in PATH: %w", err)
		}
		cfg.PgBackRest.Bin = bin
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

func basePgBackRestArgs(cfg *Config) []string {
	args := []string{
		"--stanza=" + cfg.PgBackRest.Stanza,
	}
	if cfg.PgBackRest.ConfigFile != "" {
		args = append(args, "--config="+cfg.PgBackRest.ConfigFile)
	}
	args = append(args, cfg.PgBackRest.ExtraArgs...)
	return args
}

func showInfo(cfg *Config) error {
	args := append(basePgBackRestArgs(cfg), "info")
	cmd := exec.Command(cfg.PgBackRest.Bin, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	fmt.Printf("[%s] exec: %s %s\n", timeStamp(), cfg.PgBackRest.Bin, strings.Join(args, " "))
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pgbackrest info: %w", err)
	}
	return nil
}

func runBackup(cfg *Config) (*backupResult, error) {
	if err := os.MkdirAll(cfg.LogDir, 0755); err != nil {
		return nil, fmt.Errorf("create log_dir: %w", err)
	}

	ts := time.Now().Format("20060102_150405")
	logName := fmt.Sprintf("pgbackrest_%s_%s_%s.log", cfg.PgBackRest.Stanza, cfg.BackupType, ts)
	logPath := filepath.Join(cfg.LogDir, logName)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}
	defer logFile.Close()

	logger := io.MultiWriter(os.Stdout, logFile)
	fmt.Fprintf(logger, "[%s] starting pgbackrest backup: stanza=%s type=%s\n", timeStamp(), cfg.PgBackRest.Stanza, cfg.BackupType)

	args := basePgBackRestArgs(cfg)
	args = append(args, "backup", "--type="+cfg.BackupType)

	cmd := exec.Command(cfg.PgBackRest.Bin, args...)
	cmd.Stdout = logger
	cmd.Stderr = logger

	fmt.Fprintf(logger, "[%s] exec: %s %s\n", timeStamp(), cfg.PgBackRest.Bin, strings.Join(args, " "))
	if err := cmd.Run(); err != nil {
		return &backupResult{
			Stanza:     cfg.PgBackRest.Stanza,
			BackupType: cfg.BackupType,
			RepoPath:   cfg.PgBackRest.RepoPath,
			LogPath:    logPath,
		}, fmt.Errorf("pgbackrest backup: %w (see log %s)", err, logPath)
	}

	fmt.Fprintf(logger, "[%s] backup finished\n", timeStamp())
	return &backupResult{
		Stanza:      cfg.PgBackRest.Stanza,
		BackupType:  cfg.BackupType,
		BackupLabel: ts,
		RepoPath:    cfg.PgBackRest.RepoPath,
		LogPath:     logPath,
	}, nil
}

func cleanupOldLogs(cfg *Config) error {
	entries, err := os.ReadDir(cfg.LogDir)
	if err != nil {
		return fmt.Errorf("cleanup read log_dir: %w", err)
	}
	cutoff := time.Now().AddDate(0, 0, -cfg.RetentionDays)
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(cfg.LogDir, e.Name()))
		}
	}
	return nil
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

// 发送飞书消息（卡片优先，失败回退 text）
func sendFeishu(cfg *Config, res *backupResult, status string, errMsg string) {
	if !cfg.Feishu.Enabled {
		return
	}

	stanza := cfg.PgBackRest.Stanza
	backupType := cfg.BackupType
	repoPath := cfg.PgBackRest.RepoPath
	logPath := ""
	if res != nil {
		stanza = res.Stanza
		backupType = res.BackupType
		if res.RepoPath != "" {
			repoPath = res.RepoPath
		}
		logPath = res.LogPath
	}

	titlePrefix := ""
	if cfg.Feishu.Keyword != "" {
		titlePrefix = "[" + cfg.Feishu.Keyword + "] "
	}
	title := titlePrefix + "PostgreSQL 备份" + status + ": " + stanza

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
		fmt.Sprintf("**stanza**：%s", stanza),
		fmt.Sprintf("**类型**：%s", backupType),
		fmt.Sprintf("**备份仓库**：%s", repoPath),
		fmt.Sprintf("**日志**：%s", logPath),
		fmt.Sprintf("**时间**：%s", time.Now().Format("2006-01-02 15:04:05")),
	)
	if errMsg != "" {
		mdLines = append(mdLines, fmt.Sprintf("**错误**：`%s`", strings.ReplaceAll(errMsg, "`", "'")))
	}
	md := strings.Join(mdLines, "\n")

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
		fmt.Sprintf("stanza: %s", stanza),
		fmt.Sprintf("类型: %s", backupType),
		fmt.Sprintf("日志: %s", logPath),
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
