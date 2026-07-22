package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Config struct {
	ClickHouseBackup struct {
		Mode string `json:"mode"` // api / ssh

		Scheme   string   `json:"scheme"`
		Port     int      `json:"port"`
		Username string   `json:"username"`
		Password string   `json:"password"`
		Nodes    []string `json:"nodes"`

		RequestTimeoutSeconds int `json:"request_timeout_seconds"`
		PollIntervalSeconds   int `json:"poll_interval_seconds"`
		PollTimeoutSeconds    int `json:"poll_timeout_seconds"`

		SSH struct {
			User         string   `json:"user"`
			Port         int      `json:"port"`
			IdentityFile string   `json:"identity_file"`
			Bin          string   `json:"bin"`
			ExtraArgs    []string `json:"extra_args"`
			BatchMode    *bool    `json:"batch_mode"`
		} `json:"ssh"`
	} `json:"clickhouse_backup"`

	Backup struct {
		Type       string `json:"type"` // full / incr
		NamePrefix string `json:"name_prefix"`
		// Create 阶段的参数，对应 clickhouse-backup 的接口 query 参数：
		// 例如 curl ".../backup/create?name=xxx&configs=true"
		Create struct {
			Configs bool `json:"configs"`
		} `json:"create"`

		Upload struct {
			Enabled        *bool  `json:"enabled"`
			DiffFrom       string `json:"diff_from"`
			DiffFromRemote string `json:"diff_from_remote"`
			Resumable      bool   `json:"resumable"`
			DeleteSource   bool   `json:"delete_source"`
		} `json:"upload"`
	} `json:"backup"`

	Log struct {
		Dir string `json:"dir"`
	} `json:"log"`

	Feishu struct {
		Enabled bool   `json:"enabled"`
		Webhook string `json:"webhook"`
		Keyword string `json:"keyword"`
	} `json:"feishu"`
}

type NodeResult struct {
	Node  string
	Phase string
	Err   error
}

type ClusterSummary struct {
	BackupName string
	Phase      string
	Successes  []string
	Failures   []NodeResult
	LogPath    string
}

func main() {
	cmd, args := parseCommand(os.Args[1:])
	switch cmd {
	case "backup":
		if err := runBackupCommand(args); err != nil {
			os.Exit(1)
		}
	case "list":
		if err := runListCommand(args); err != nil {
			os.Exit(1)
		}
	default:
		usage()
		os.Exit(2)
	}
}

func parseCommand(args []string) (string, []string) {
	if len(args) == 0 {
		return "backup", args
	}
	// 兼容两种写法：
	// 1) ck-cluster-backup list -c config.json
	// 2) ck-cluster-backup -c config.json list
	for i, a := range args {
		if strings.HasPrefix(a, "-") {
			continue
		}
		if a == "backup" || a == "list" {
			rest := append([]string{}, args[:i]...)
			rest = append(rest, args[i+1:]...)
			return a, rest
		}
		break
	}
	return "backup", args
}

func usage() {
	fmt.Println("Usage:")
	fmt.Println("  ck-cluster-backup backup -config config/ck_cluster_backup.json [-type full|incr] [-name xxx]")
	fmt.Println("  ck-cluster-backup backup -c      config/ck_cluster_backup.json [-type full|incr] [-name xxx]")
	fmt.Println("  ck-cluster-backup list   -config config/ck_cluster_backup.json")
	fmt.Println("  ck-cluster-backup list   -c      config/ck_cluster_backup.json")
	fmt.Println("")
	fmt.Println("也支持省略子命令，默认执行 backup：")
	fmt.Println("  ck-cluster-backup -config config/ck_cluster_backup.json")
	fmt.Println("  ck-cluster-backup -c      config/ck_cluster_backup.json")
}

func runBackupCommand(args []string) error {
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	var cfgPath string
	var backupTypeOverride string
	var nameOverride string
	var nodesOverride string
	var portOverride int
	var userOverride string
	var passOverride string
	var schemeOverride string
	var diffFromOverride string
	var diffFromRemoteOverride string
	var skipFeishu bool
	var dryRun bool

	fs.StringVar(&cfgPath, "config", "config/ck_cluster_backup.json", "配置文件路径 (JSON)")
	fs.StringVar(&cfgPath, "c", "config/ck_cluster_backup.json", "config 简写")
	fs.StringVar(&backupTypeOverride, "type", "", "备份类型：full 普通完整上传，incr 自动基于最近远端备份增量上传")
	fs.StringVar(&nameOverride, "name", "", "指定备份名称（不传则自动生成）")
	fs.StringVar(&nodesOverride, "nodes", "", "覆盖节点列表，逗号分隔，例如 10.0.0.1,10.0.0.2")
	fs.IntVar(&portOverride, "port", 0, "覆盖 API 端口")
	fs.StringVar(&schemeOverride, "scheme", "", "覆盖 scheme：http/https")
	fs.StringVar(&userOverride, "user", "", "覆盖 Basic Auth 用户名")
	fs.StringVar(&passOverride, "pass", "", "覆盖 Basic Auth 密码")
	fs.StringVar(&diffFromOverride, "diff-from", "", "增量上传基线：本地备份名（对应 clickhouse-backup upload --diff-from）")
	fs.StringVar(&diffFromRemoteOverride, "diff-from-remote", "", "增量上传基线：远端备份名（对应 clickhouse-backup upload --diff-from-remote）")
	fs.BoolVar(&skipFeishu, "skip-feishu", false, "跳过飞书通知（即使配置开启）")
	fs.BoolVar(&dryRun, "dry-run", false, "只打印计划执行内容，不实际调用 API")

	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := loadConfig(cfgPath)
	if err != nil {
		logError("加载配置失败: %v", err)
		return err
	}
	applyOverrides(cfg, nodesOverride, portOverride, schemeOverride, userOverride, passOverride)
	if diffFromOverride != "" {
		cfg.Backup.Upload.DiffFrom = diffFromOverride
	}
	if diffFromRemoteOverride != "" {
		cfg.Backup.Upload.DiffFromRemote = diffFromRemoteOverride
	}
	if backupTypeOverride == "" && (diffFromOverride != "" || diffFromRemoteOverride != "") {
		cfg.Backup.Type = "incr"
	}
	if err := applyBackupType(cfg, backupTypeOverride); err != nil {
		logError("备份类型不合法: %v", err)
		return err
	}
	if err := validateConfig(cfg); err != nil {
		logError("配置不合法: %v", err)
		return err
	}

	backupName := nameOverride
	if backupName == "" {
		backupName = generateBackupName(cfg)
	}

	logPath := ""
	logWriter := io.Writer(os.Stdout)
	if cfg.Log.Dir != "" {
		if err := os.MkdirAll(cfg.Log.Dir, 0755); err != nil {
			logError("创建日志目录失败: %v", err)
			return err
		}
		logPath = filepath.Join(cfg.Log.Dir, fmt.Sprintf("ck_cluster_%s.log", time.Now().Format("20060102_150405")))
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			logError("创建日志文件失败: %v", err)
			return err
		}
		defer f.Close()
		logWriter = io.MultiWriter(os.Stdout, f)
	}

	logInfoW(logWriter, "生成备份名: %s", backupName)
	logInfoW(logWriter, "备份类型: %s", cfg.Backup.Type)
	logInfoW(logWriter, "节点数: %d, port=%d, scheme=%s", len(cfg.ClickHouseBackup.Nodes), cfg.ClickHouseBackup.Port, cfg.ClickHouseBackup.Scheme)
	if params := buildUploadParams(cfg); len(params) > 0 {
		logInfoW(logWriter, "upload params: %v", params)
	}
	if dryRun {
		logInfoW(logWriter, "dry-run: 将对所有节点执行 Phase1=create，全部成功后执行 Phase2=upload，并使用 /backup/status 轮询等待 success")
		return nil
	}

	clients := make([]*Client, 0, len(cfg.ClickHouseBackup.Nodes))
	for _, node := range cfg.ClickHouseBackup.Nodes {
		if cfg.ClickHouseBackup.Mode == "ssh" {
			clients = append(clients, &Client{Node: node})
		} else {
			clients = append(clients, NewClient(cfg, node))
		}
	}

	ctx := context.Background()
	createSummary := runPhase(ctx, logWriter, cfg, clients, backupName, "create")
	createSummary.LogPath = logPath
	if len(createSummary.Failures) > 0 {
		logErrorW(logWriter, "Create 阶段存在失败节点，跳过 Upload。")
		if cfg.Feishu.Enabled && !skipFeishu {
			sendFeishu(cfg, createSummary, "失败", fmt.Sprintf("Create 阶段失败节点数=%d", len(createSummary.Failures)))
		}
		printSummary(logWriter, createSummary)
		return errors.New("create phase failed")
	}

	if cfg.Backup.Upload.Enabled != nil && !*cfg.Backup.Upload.Enabled {
		logInfoW(logWriter, "upload.enabled=false，已跳过 Upload 阶段（不会上传到远端仓库/MinIO）。")
		if cfg.Feishu.Enabled && !skipFeishu {
			sendFeishu(cfg, createSummary, "成功", "已跳过 Upload（upload.enabled=false）")
		}
		printSummary(logWriter, createSummary)
		return nil
	}

	uploadSummary := runPhase(ctx, logWriter, cfg, clients, backupName, "upload")
	uploadSummary.LogPath = logPath
	if cfg.Feishu.Enabled && !skipFeishu {
		status := "成功"
		msg := "Create+Upload 全部成功"
		if len(uploadSummary.Failures) > 0 {
			status = "失败"
			msg = fmt.Sprintf("Upload 阶段失败节点数=%d", len(uploadSummary.Failures))
		}
		sendFeishu(cfg, uploadSummary, status, msg)
	}
	printSummary(logWriter, uploadSummary)
	if len(uploadSummary.Failures) > 0 {
		return errors.New("upload phase failed")
	}
	return nil
}

func runListCommand(args []string) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	var cfgPath string
	var nodesOverride string
	var portOverride int
	var userOverride string
	var passOverride string
	var schemeOverride string

	fs.StringVar(&cfgPath, "config", "config/ck_cluster_backup.json", "配置文件路径 (JSON)")
	fs.StringVar(&cfgPath, "c", "config/ck_cluster_backup.json", "config 简写")
	fs.StringVar(&nodesOverride, "nodes", "", "覆盖节点列表，逗号分隔，例如 10.0.0.1,10.0.0.2")
	fs.IntVar(&portOverride, "port", 0, "覆盖 API 端口")
	fs.StringVar(&schemeOverride, "scheme", "", "覆盖 scheme：http/https")
	fs.StringVar(&userOverride, "user", "", "覆盖 Basic Auth 用户名")
	fs.StringVar(&passOverride, "pass", "", "覆盖 Basic Auth 密码")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := loadConfig(cfgPath)
	if err != nil {
		logError("加载配置失败: %v", err)
		return err
	}
	applyOverrides(cfg, nodesOverride, portOverride, schemeOverride, userOverride, passOverride)
	if err := validateConfig(cfg); err != nil {
		logError("配置不合法: %v", err)
		return err
	}

	clients := make([]*Client, 0, len(cfg.ClickHouseBackup.Nodes))
	for _, node := range cfg.ClickHouseBackup.Nodes {
		if cfg.ClickHouseBackup.Mode == "ssh" {
			clients = append(clients, &Client{Node: node})
		} else {
			clients = append(clients, NewClient(cfg, node))
		}
	}

	type listRes struct {
		Node    string
		Backups []string
		Err     error
	}

	var (
		wg  sync.WaitGroup
		mu  sync.Mutex
		out []listRes
	)
	for _, c := range clients {
		wg.Add(1)
		go func(cl *Client) {
			defer wg.Done()
			var (
				backups []string
				err     error
			)
			if cfg.ClickHouseBackup.Mode == "ssh" {
				sshc := NewSSHClient(cfg, cl.Node)
				backups, err = sshc.List(context.Background(), io.Discard)
			} else {
				backups, err = cl.ListBackups(context.Background())
			}
			mu.Lock()
			out = append(out, listRes{Node: cl.Node, Backups: backups, Err: err})
			mu.Unlock()
		}(c)
	}
	wg.Wait()

	sort.Slice(out, func(i, j int) bool { return out[i].Node < out[j].Node })
	for _, r := range out {
		if r.Err != nil {
			logError("[%s] list 失败: %v", r.Node, r.Err)
			continue
		}
		fmt.Printf("== %s ==\n", r.Node)
		for _, b := range r.Backups {
			fmt.Println("  " + b)
		}
	}
	return nil
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

func applyOverrides(cfg *Config, nodesCSV string, port int, scheme, user, pass string) {
	if nodesCSV != "" {
		cfg.ClickHouseBackup.Nodes = splitCSV(nodesCSV)
	}
	if port != 0 {
		cfg.ClickHouseBackup.Port = port
	}
	if scheme != "" {
		cfg.ClickHouseBackup.Scheme = scheme
	}
	if user != "" {
		cfg.ClickHouseBackup.Username = user
	}
	if pass != "" {
		cfg.ClickHouseBackup.Password = pass
	}
}

func applyBackupType(cfg *Config, backupTypeOverride string) error {
	if backupTypeOverride != "" {
		cfg.Backup.Type = backupTypeOverride
	}

	hasDiffConfig := strings.TrimSpace(cfg.Backup.Upload.DiffFrom) != "" || strings.TrimSpace(cfg.Backup.Upload.DiffFromRemote) != ""
	cfg.Backup.Type = strings.ToLower(strings.TrimSpace(cfg.Backup.Type))
	if cfg.Backup.Type == "" {
		if hasDiffConfig {
			cfg.Backup.Type = "incr"
		} else {
			cfg.Backup.Type = "full"
		}
	}

	switch cfg.Backup.Type {
	case "full":
		cfg.Backup.Upload.DiffFrom = ""
		cfg.Backup.Upload.DiffFromRemote = ""
	case "incr":
		if !hasDiffConfig {
			cfg.Backup.Upload.DiffFromRemote = "latest-full"
		}
	default:
		return fmt.Errorf("backup.type 只能是 full 或 incr，当前=%s", cfg.Backup.Type)
	}
	return nil
}

func validateConfig(cfg *Config) error {
	if cfg.ClickHouseBackup.Mode == "" {
		cfg.ClickHouseBackup.Mode = "api"
	}
	if cfg.ClickHouseBackup.Mode != "api" && cfg.ClickHouseBackup.Mode != "ssh" {
		return fmt.Errorf("clickhouse_backup.mode 只能是 api 或 ssh，当前=%s", cfg.ClickHouseBackup.Mode)
	}

	if cfg.ClickHouseBackup.Scheme == "" {
		cfg.ClickHouseBackup.Scheme = "http"
	}
	if cfg.ClickHouseBackup.Port == 0 {
		cfg.ClickHouseBackup.Port = 7171
	}
	if len(cfg.ClickHouseBackup.Nodes) == 0 {
		return errors.New("clickhouse_backup.nodes 不能为空")
	}
	if cfg.ClickHouseBackup.Mode == "api" {
		if cfg.ClickHouseBackup.Username == "" || cfg.ClickHouseBackup.Password == "" {
			return errors.New("clickhouse_backup.username/password 不能为空（Basic Auth）")
		}
	}
	if cfg.ClickHouseBackup.Mode == "ssh" {
		if cfg.ClickHouseBackup.SSH.User == "" {
			return errors.New("clickhouse_backup.ssh.user 不能为空（SSH 模式）")
		}
		if cfg.ClickHouseBackup.SSH.Port == 0 {
			cfg.ClickHouseBackup.SSH.Port = 22
		}
		if cfg.ClickHouseBackup.SSH.Bin == "" {
			cfg.ClickHouseBackup.SSH.Bin = "clickhouse-backup"
		}
		// 默认启用 BatchMode，避免交互式密码提示
		if cfg.ClickHouseBackup.SSH.BatchMode == nil {
			v := true
			cfg.ClickHouseBackup.SSH.BatchMode = &v
		}
		if _, err := exec.LookPath("ssh"); err != nil {
			return fmt.Errorf("ssh 命令不存在: %w", err)
		}
	}
	if cfg.ClickHouseBackup.RequestTimeoutSeconds == 0 {
		cfg.ClickHouseBackup.RequestTimeoutSeconds = 30
	}
	if cfg.ClickHouseBackup.PollIntervalSeconds == 0 {
		cfg.ClickHouseBackup.PollIntervalSeconds = 3
	}
	if cfg.ClickHouseBackup.PollTimeoutSeconds == 0 {
		cfg.ClickHouseBackup.PollTimeoutSeconds = 1800
	}
	if cfg.Backup.NamePrefix == "" {
		cfg.Backup.NamePrefix = "daily_backup"
	}
	if cfg.Backup.Upload.Enabled == nil {
		v := true
		cfg.Backup.Upload.Enabled = &v
	}
	if cfg.Backup.Upload.DiffFrom != "" && cfg.Backup.Upload.DiffFromRemote != "" {
		return errors.New("backup.upload.diff_from 和 backup.upload.diff_from_remote 不能同时设置")
	}
	if cfg.Feishu.Enabled {
		if cfg.Feishu.Webhook == "" {
			return errors.New("feishu.webhook 不能为空")
		}
		if cfg.Feishu.Keyword == "" {
			return errors.New("feishu.keyword 不能为空")
		}
	}
	return nil
}

func generateBackupName(cfg *Config) string {
	if cfg.Backup.Type != "" {
		return fmt.Sprintf("%s_%s_%s", cfg.Backup.NamePrefix, cfg.Backup.Type, time.Now().Format("20060102_150405"))
	}
	return fmt.Sprintf("%s_%s", cfg.Backup.NamePrefix, time.Now().Format("20060102_150405"))
}

type Client struct {
	Node     string
	BaseURL  string
	Username string
	Password string
	Client   *http.Client

	PollInterval time.Duration
	PollTimeout  time.Duration
}

func NewClient(cfg *Config, node string) *Client {
	timeout := time.Duration(cfg.ClickHouseBackup.RequestTimeoutSeconds) * time.Second
	return &Client{
		Node:     node,
		BaseURL:  fmt.Sprintf("%s://%s:%d", cfg.ClickHouseBackup.Scheme, node, cfg.ClickHouseBackup.Port),
		Username: cfg.ClickHouseBackup.Username,
		Password: cfg.ClickHouseBackup.Password,
		Client: &http.Client{
			Timeout: timeout,
		},
		PollInterval: time.Duration(cfg.ClickHouseBackup.PollIntervalSeconds) * time.Second,
		PollTimeout:  time.Duration(cfg.ClickHouseBackup.PollTimeoutSeconds) * time.Second,
	}
}

func (c *Client) CreateBackup(ctx context.Context, name string, params map[string]string) error {
	return c.postAction(ctx, "/backup/create", name, params)
}

func (c *Client) UploadBackup(ctx context.Context, name string, params map[string]string) error {
	return c.postAction(ctx, "/backup/upload", name, params)
}

func (c *Client) postAction(ctx context.Context, path, name string, params map[string]string) error {
	// clickhouse-backup 常见风格有两类：
	// 1) POST /backup/upload?name=xxx
	// 2) POST /backup/upload/xxx
	q := url.Values{}
	q.Set("name", name)
	for k, v := range params {
		if strings.TrimSpace(k) == "" {
			continue
		}
		q.Set(k, v)
	}
	u := c.BaseURL + path + "?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.Username, c.Password)
	resp, err := c.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		// fallback 1：路径风格 /backup/upload/<name>
		pathErr := c.postActionPath(ctx, path, name, params)
		if pathErr == nil {
			return nil
		}
		// fallback 2：JSON body {"name":"xxx"}
		jsonErr := c.postActionJSON(ctx, path, name)
		if jsonErr == nil {
			return nil
		}
		return fmt.Errorf("http %d: %s (path fallback err=%v, json fallback err=%v)", resp.StatusCode, strings.TrimSpace(string(body)), pathErr, jsonErr)
	}
	return nil
}

func (c *Client) postActionPath(ctx context.Context, path, name string, params map[string]string) error {
	u := c.BaseURL + path + "/" + url.PathEscape(name)
	if len(params) > 0 {
		q := url.Values{}
		for k, v := range params {
			if strings.TrimSpace(k) == "" {
				continue
			}
			q.Set(k, v)
		}
		if qs := q.Encode(); qs != "" {
			u += "?" + qs
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.Username, c.Password)
	resp, err := c.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func (c *Client) postActionJSON(ctx context.Context, path, name string) error {
	payload := map[string]string{"name": name}
	data, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(c.Username, c.Password)
	resp, err := c.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

type statusSnapshot struct {
	Status      string
	Command     string
	Progress    string
	OperationID string
	Raw         string
}

type backupListItem struct {
	Name    string
	Created time.Time
	Broken  bool
}

func (c *Client) GetStatus(ctx context.Context) (statusSnapshot, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/backup/status", nil)
	if err != nil {
		return statusSnapshot{}, err
	}
	req.SetBasicAuth(c.Username, c.Password)
	resp, err := c.Client.Do(req)
	if err != nil {
		return statusSnapshot{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	raw := strings.TrimSpace(string(body))
	if resp.StatusCode >= 300 {
		return statusSnapshot{}, fmt.Errorf("http %d: %s", resp.StatusCode, raw)
	}

	// status 做兼容json和字符串解析。
	var m map[string]interface{}
	if err := json.Unmarshal(body, &m); err == nil {
		status := firstString(m, "status", "state")
		cmd := firstString(m, "command", "action", "operation")
		opID := firstString(m, "operation_id", "operationId", "op_id", "opId")
		progress := ""
		if v, ok := m["progress"]; ok {
			progress = fmt.Sprint(v)
		}
		return statusSnapshot{
			Status:      strings.ToLower(status),
			Command:     cmd,
			Progress:    progress,
			OperationID: opID,
			Raw:         raw,
		}, nil
	}

	// 非 JSON：尝试从文本里提取关键字
	return statusSnapshot{Status: strings.ToLower(raw), Raw: raw}, nil
}

func (c *Client) WaitSuccess(ctx context.Context, phase string, backupName string, prev statusSnapshot, w io.Writer) error {
	deadline := time.Now().Add(c.PollTimeout)
	seenNewOperation := false
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("轮询超时（phase=%s, timeout=%s）", phase, c.PollTimeout)
		}

		s, err := c.GetStatus(ctx)
		if err != nil {
			logWarnW(w, "[%s] status 获取失败: %v（继续重试）", c.Node, err)
			time.Sleep(c.PollInterval)
			continue
		}

		// 避免被“上一次任务 success”误判：必须观测到本次备份相关的 command/operation_id 后，才开始接受 success 作为完成。
		if !seenNewOperation {
			if isSameOperation(prev, s) {
				logInfoW(w, "[%s] phase=%s 等待本次任务开始（仍是上一次 operation）", c.Node, phase)
				time.Sleep(c.PollInterval)
				continue
			}
			if !statusMatchesBackup(phase, backupName, s) {
				logInfoW(w, "[%s] phase=%s 等待本次任务开始（当前 command=%q）", c.Node, phase, s.Command)
				time.Sleep(c.PollInterval)
				continue
			}
			seenNewOperation = true
		}

		st := normalizeStatus(s.Status)
		switch st {
		case "success", "done", "ok":
			logInfoW(w, "[%s] phase=%s status=success op=%s", c.Node, phase, s.OperationID)
			return nil
		case "failed", "error", "fail":
			return fmt.Errorf("phase=%s status=%s raw=%s", phase, st, s.Raw)
		default:
			// in progress
			if s.Progress != "" {
				logInfoW(w, "[%s] phase=%s status=%s progress=%s", c.Node, phase, st, s.Progress)
			} else {
				logInfoW(w, "[%s] phase=%s status=%s", c.Node, phase, st)
			}
			time.Sleep(c.PollInterval)
		}
	}
}

func isSameOperation(prev, cur statusSnapshot) bool {
	// 如果 operation_id 可用，用它判断；否则用 command 判断。
	if prev.OperationID != "" && cur.OperationID != "" {
		return prev.OperationID == cur.OperationID
	}
	if prev.Command != "" && cur.Command != "" {
		return prev.Command == cur.Command
	}
	return false
}

func statusMatchesBackup(phase, backupName string, s statusSnapshot) bool {
	// clickhouse-backup status 返回的 command 通常包含：
	//   create --configs <backupName>
	//   upload <backupName>
	cmd := strings.ToLower(s.Command)
	if backupName != "" && (strings.Contains(s.Raw, backupName) || strings.Contains(s.Command, backupName)) {
		// 校验一下动作关键字
		switch phase {
		case "create":
			return strings.Contains(cmd, "create")
		case "upload":
			return strings.Contains(cmd, "upload")
		default:
			return true
		}
	}
	return false
}

func (c *Client) ListBackups(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/backup/list", nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.Username, c.Password)
	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	raw := strings.TrimSpace(string(body))
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, raw)
	}

	// 兼容不同返回格式：
	// 1) ["a","b"]
	// 2) {"backups":["a","b"]}
	// 3) {"data":["a","b"]}
	var arr []string
	if err := json.Unmarshal(body, &arr); err == nil {
		sort.Strings(arr)
		return arr, nil
	}

	var obj map[string]interface{}
	if err := json.Unmarshal(body, &obj); err == nil {
		for _, k := range []string{"backups", "data", "result"} {
			if v, ok := obj[k]; ok {
				if list := toStringSlice(v); len(list) > 0 {
					sort.Strings(list)
					return list, nil
				}
			}
		}
	}

	// 无法解析：按行拆
	lines := []string{}
	for _, ln := range strings.Split(raw, "\n") {
		ln = strings.TrimSpace(ln)
		if ln != "" {
			lines = append(lines, ln)
		}
	}
	sort.Strings(lines)
	return lines, nil
}

func (c *Client) ListRemoteBackups(ctx context.Context) ([]backupListItem, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/backup/list/remote", nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.Username, c.Password)
	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	raw := strings.TrimSpace(string(body))
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, raw)
	}
	return parseBackupList(body), nil
}

type SSHClient struct {
	Node         string
	User         string
	Port         int
	IdentityFile string
	Bin          string
	ExtraArgs    []string
	BatchMode    bool
}

func NewSSHClient(cfg *Config, node string) *SSHClient {
	bin := strings.TrimSpace(cfg.ClickHouseBackup.SSH.Bin)
	if bin == "" {
		bin = "clickhouse-backup"
	}
	batchMode := true
	if cfg.ClickHouseBackup.SSH.BatchMode != nil {
		batchMode = *cfg.ClickHouseBackup.SSH.BatchMode
	}
	return &SSHClient{
		Node:         node,
		User:         cfg.ClickHouseBackup.SSH.User,
		Port:         cfg.ClickHouseBackup.SSH.Port,
		IdentityFile: cfg.ClickHouseBackup.SSH.IdentityFile,
		Bin:          bin,
		ExtraArgs:    append([]string(nil), cfg.ClickHouseBackup.SSH.ExtraArgs...),
		BatchMode:    batchMode,
	}
}

func (s *SSHClient) run(ctx context.Context, w io.Writer, remoteArgs []string) error {
	base := []string{"-p", fmt.Sprint(s.Port)}
	if s.IdentityFile != "" {
		base = append(base, "-i", s.IdentityFile)
	}
	if s.BatchMode {
		base = append(base, "-o", "BatchMode=yes")
	}
	base = append(base, s.ExtraArgs...)
	base = append(base, fmt.Sprintf("%s@%s", s.User, s.Node))
	base = append(base, remoteArgs...)

	cmd := exec.CommandContext(ctx, "ssh", base...)
	cmd.Stdout = w
	cmd.Stderr = w
	logInfoW(w, "[%s] exec: ssh %s", s.Node, strings.Join(base, " "))
	return cmd.Run()
}

func (s *SSHClient) Create(ctx context.Context, w io.Writer, name string, configs bool) error {
	args := []string{s.Bin, "create", name}
	if configs {
		args = append(args, "--configs")
	}
	return s.run(ctx, w, args)
}

func (s *SSHClient) Upload(ctx context.Context, w io.Writer, name string, params map[string]string) error {
	args := []string{s.Bin, "upload"}
	if v := strings.TrimSpace(params["diff-from"]); v != "" {
		args = append(args, "--diff-from="+v)
	}
	if v := strings.TrimSpace(params["diff-from-remote"]); v != "" {
		args = append(args, "--diff-from-remote="+v)
	}
	if params["resumable"] == "true" {
		args = append(args, "--resumable")
	}
	if params["delete-source"] == "true" {
		args = append(args, "--delete-source")
	}
	args = append(args, name)
	return s.run(ctx, w, args)
}

func (s *SSHClient) List(ctx context.Context, w io.Writer) ([]string, error) {
	var buf bytes.Buffer
	mw := io.MultiWriter(w, &buf)
	if err := s.run(ctx, mw, []string{s.Bin, "list"}); err != nil {
		return nil, err
	}
	lines := []string{}
	for _, ln := range strings.Split(buf.String(), "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		if strings.HasPrefix(strings.ToLower(ln), "backups") {
			continue
		}
		lines = append(lines, ln)
	}
	sort.Strings(lines)
	return lines, nil
}

func (s *SSHClient) ListRemoteBackups(ctx context.Context, w io.Writer) ([]backupListItem, error) {
	var buf bytes.Buffer
	mw := io.MultiWriter(w, &buf)
	if err := s.run(ctx, mw, []string{s.Bin, "list", "remote"}); err != nil {
		return nil, err
	}
	return parseBackupList(buf.Bytes()), nil
}

func runPhase(ctx context.Context, w io.Writer, cfg *Config, clients []*Client, backupName, phase string) ClusterSummary {
	logInfoW(w, "== Phase: %s ==", phase)
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		success  []string
		failures []NodeResult
	)
	for _, cl := range clients {
		wg.Add(1)
		go func(c *Client) {
			defer wg.Done()

			start := time.Now()
			var err error
			switch phase {
			case "create":
				params := map[string]string{}
				if cfg.Backup.Create.Configs {
					params["configs"] = "true"
				}
				if cfg.ClickHouseBackup.Mode == "ssh" {
					sshc := NewSSHClient(cfg, c.Node)
					logInfoW(w, "[%s] ssh create name=%s configs=%v", c.Node, backupName, cfg.Backup.Create.Configs)
					err = sshc.Create(ctx, w, backupName, cfg.Backup.Create.Configs)
				} else {
					prev, _ := c.GetStatus(ctx)
					logInfoW(w, "[%s] POST /backup/create name=%s", c.Node, backupName)
					if len(params) > 0 {
						logInfoW(w, "[%s] create params: %v", c.Node, params)
					}
					err = c.CreateBackup(ctx, backupName, params)
					if err == nil {
						err = c.WaitSuccess(ctx, phase, backupName, prev, w)
					}
				}
			case "upload":
				params, err := resolveUploadParams(ctx, w, cfg, c, backupName)
				if err != nil {
					break
				}
				if cfg.ClickHouseBackup.Mode == "ssh" {
					sshc := NewSSHClient(cfg, c.Node)
					logInfoW(w, "[%s] ssh upload name=%s", c.Node, backupName)
					if len(params) > 0 {
						logInfoW(w, "[%s] upload params: %v", c.Node, params)
					}
					err = sshc.Upload(ctx, w, backupName, params)
				} else {
					prev, _ := c.GetStatus(ctx)
					logInfoW(w, "[%s] POST /backup/upload name=%s", c.Node, backupName)
					if len(params) > 0 {
						logInfoW(w, "[%s] upload params: %v", c.Node, params)
					}
					err = c.UploadBackup(ctx, backupName, params)
					if err == nil {
						err = c.WaitSuccess(ctx, phase, backupName, prev, w)
					}
				}
			default:
				err = fmt.Errorf("unknown phase: %s", phase)
			}
			// API 模式的轮询已经在各分支里处理；SSH 模式依赖命令退出码。

			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failures = append(failures, NodeResult{Node: c.Node, Phase: phase, Err: err})
				logErrorW(w, "[%s] phase=%s 失败: %v (耗时 %s)", c.Node, phase, err, time.Since(start).Truncate(time.Second))
				return
			}
			success = append(success, c.Node)
			logInfoW(w, "[%s] phase=%s 成功 (耗时 %s)", c.Node, phase, time.Since(start).Truncate(time.Second))
		}(cl)
	}
	wg.Wait()

	sort.Strings(success)
	sort.Slice(failures, func(i, j int) bool { return failures[i].Node < failures[j].Node })
	return ClusterSummary{
		BackupName: backupName,
		Phase:      phase,
		Successes:  success,
		Failures:   failures,
	}
}

func printSummary(w io.Writer, s ClusterSummary) {
	logInfoW(w, "== Summary ==")
	logInfoW(w, "backup=%s phase=%s ok=%d fail=%d", s.BackupName, s.Phase, len(s.Successes), len(s.Failures))
	if s.LogPath != "" {
		logInfoW(w, "log=%s", s.LogPath)
	}
	if len(s.Failures) > 0 {
		for _, f := range s.Failures {
			logErrorW(w, "FAILED node=%s phase=%s err=%v", f.Node, f.Phase, f.Err)
		}
	}
}

// --- 飞书通知（卡片优先，失败回退 text） ---

func sendFeishu(cfg *Config, s ClusterSummary, status string, msg string) {
	titlePrefix := ""
	if cfg.Feishu.Keyword != "" {
		titlePrefix = "[" + cfg.Feishu.Keyword + "] "
	}
	title := titlePrefix + "ClickHouse 集群备份" + status + ": " + s.BackupName

	host, _ := os.Hostname()
	ip := getLocalIP()
	color := "orange"
	switch status {
	case "成功":
		color = "green"
	case "失败":
		color = "red"
	}

	lines := []string{
		cfg.Feishu.Keyword,
		fmt.Sprintf("**状态**：%s", status),
		fmt.Sprintf("**主机**：%s", host),
		fmt.Sprintf("**IP**：%s", ip),
		fmt.Sprintf("**备份名**：%s", s.BackupName),
		fmt.Sprintf("**阶段**：%s", s.Phase),
		fmt.Sprintf("**成功节点**：%d", len(s.Successes)),
		fmt.Sprintf("**失败节点**：%d", len(s.Failures)),
		fmt.Sprintf("**时间**：%s", time.Now().Format("2006-01-02 15:04:05")),
	}
	if s.LogPath != "" {
		lines = append(lines, fmt.Sprintf("**日志**：%s", s.LogPath))
	}
	if msg != "" {
		lines = append(lines, fmt.Sprintf("**说明**：%s", sanitizeBackticks(msg)))
	}
	if len(s.Failures) > 0 {
		for _, f := range s.Failures {
			lines = append(lines, fmt.Sprintf("- %s: %s", f.Node, sanitizeBackticks(f.Err.Error())))
		}
	}
	md := strings.Join(lines, "\n")

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

	textPayload := map[string]interface{}{
		"msg_type": "text",
		"content": map[string]string{
			"text": strings.ReplaceAll(md, "**", ""),
		},
	}
	if err := postFeishuWebhook(cfg.Feishu.Webhook, textPayload); err != nil {
		logError("send feishu failed: %v", err)
	}
}

func postFeishuWebhook(webhook string, payload map[string]interface{}) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, webhook, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return nil
}

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

func sanitizeBackticks(s string) string {
	return strings.ReplaceAll(s, "`", "'")
}

// --- 小工具函数 ---

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func firstString(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
	}
	return ""
}

func toStringSlice(v interface{}) []string {
	switch vv := v.(type) {
	case []interface{}:
		out := make([]string, 0, len(vv))
		for _, x := range vv {
			out = append(out, fmt.Sprint(x))
		}
		return out
	case []string:
		return append([]string(nil), vv...)
	default:
		return nil
	}
}

func buildUploadParams(cfg *Config) map[string]string {
	params := map[string]string{}
	if v := strings.TrimSpace(cfg.Backup.Upload.DiffFrom); v != "" {
		params["diff-from"] = v
	}
	if v := strings.TrimSpace(cfg.Backup.Upload.DiffFromRemote); v != "" {
		params["diff-from-remote"] = v
	}
	if cfg.Backup.Upload.Resumable {
		params["resumable"] = "true"
	}
	if cfg.Backup.Upload.DeleteSource {
		params["delete-source"] = "true"
	}
	return params
}

func resolveUploadParams(ctx context.Context, w io.Writer, cfg *Config, c *Client, backupName string) (map[string]string, error) {
	params := buildUploadParams(cfg)
	autoMode := autoDiffFromRemoteMode(cfg.Backup.Upload.DiffFromRemote)
	if autoMode == "" {
		return params, nil
	}

	items, err := listRemoteBackupsForNode(ctx, w, cfg, c)
	if err != nil {
		return nil, fmt.Errorf("自动识别远端增量基线失败: %w", err)
	}
	latest := selectLatestRemoteBackup(items, cfg.Backup.NamePrefix, backupName, autoMode)
	if latest == "" {
		delete(params, "diff-from-remote")
		logInfoW(w, "[%s] 未找到可用远端基线，本次按普通完整上传执行", c.Node)
		return params, nil
	}
	params["diff-from-remote"] = latest
	logInfoW(w, "[%s] 自动选择远端增量基线: %s", c.Node, latest)
	return params, nil
}

func listRemoteBackupsForNode(ctx context.Context, w io.Writer, cfg *Config, c *Client) ([]backupListItem, error) {
	if cfg.ClickHouseBackup.Mode == "ssh" {
		return NewSSHClient(cfg, c.Node).ListRemoteBackups(ctx, w)
	}
	return c.ListRemoteBackups(ctx)
}

func autoDiffFromRemoteMode(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "auto", "latest":
		return "latest"
	case "latest-full", "latest_full", "full":
		return "latest-full"
	default:
		return ""
	}
}

func selectLatestRemoteBackup(items []backupListItem, prefix, excludeName, mode string) string {
	if mode == "latest-full" {
		best := selectLatestRemoteBackupWithPrefix(items, prefix+"_full", excludeName)
		if best != "" {
			return best
		}
	}
	best := selectLatestRemoteBackupWithPrefix(items, prefix, excludeName)
	if best != "" {
		return best
	}
	return selectLatestRemoteBackupWithPrefix(items, "", excludeName)
}

func selectLatestRemoteBackupWithPrefix(items []backupListItem, prefix, excludeName string) string {
	var best backupListItem
	for _, it := range items {
		if it.Name == "" || it.Name == excludeName || it.Broken {
			continue
		}
		if prefix != "" && !strings.HasPrefix(it.Name, prefix+"_") {
			continue
		}
		if it.Created.IsZero() {
			it.Created = parseBackupTimeFromName(it.Name)
		}
		if best.Name == "" || backupItemAfter(it, best) {
			best = it
		}
	}
	return best.Name
}

func backupItemAfter(a, b backupListItem) bool {
	if !a.Created.IsZero() && !b.Created.IsZero() {
		return a.Created.After(b.Created)
	}
	if !a.Created.IsZero() {
		return true
	}
	if !b.Created.IsZero() {
		return false
	}
	return a.Name > b.Name
}

func parseBackupList(data []byte) []backupListItem {
	var arr []interface{}
	if err := json.Unmarshal(data, &arr); err == nil {
		out := make([]backupListItem, 0, len(arr))
		for _, v := range arr {
			switch vv := v.(type) {
			case string:
				out = append(out, backupListItem{Name: strings.TrimSpace(vv), Created: parseBackupTimeFromName(vv)})
			case map[string]interface{}:
				out = append(out, backupListItem{
					Name:    firstString(vv, "backup_name", "backupName", "name", "Name"),
					Created: firstTime(vv, "created", "Created", "creation_date", "creationDate", "time", "Time", "date", "Date"),
					Broken:  backupListItemBroken(vv),
				})
			}
		}
		return out
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	out := make([]backupListItem, 0, len(lines))
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "[") || strings.HasPrefix(strings.ToLower(ln), "backups") {
			continue
		}
		fields := strings.Fields(ln)
		if len(fields) == 0 {
			continue
		}
		name := fields[0]
		if name == "Name" || name == "backup_name" {
			continue
		}
		out = append(out, backupListItem{
			Name:    name,
			Created: parseBackupTimeFromLine(ln, name),
			Broken:  strings.Contains(strings.ToLower(ln), "broken"),
		})
	}
	return out
}

func backupListItemBroken(m map[string]interface{}) bool {
	for _, key := range []string{"broken", "Broken"} {
		if v, ok := m[key]; ok {
			if b, ok := v.(bool); ok {
				return b
			}
			if strings.EqualFold(fmt.Sprint(v), "true") {
				return true
			}
		}
	}
	for _, key := range []string{"status", "Status", "error", "Error"} {
		if v, ok := m[key]; ok {
			s := strings.ToLower(fmt.Sprint(v))
			if strings.Contains(s, "broken") || strings.Contains(s, "error") {
				return true
			}
		}
	}
	return false
}

func firstTime(m map[string]interface{}, keys ...string) time.Time {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if t := parseTimeString(fmt.Sprint(v)); !t.IsZero() {
				return t
			}
		}
	}
	return time.Time{}
}

func parseBackupTimeFromLine(line, name string) time.Time {
	line = strings.TrimSpace(strings.TrimPrefix(line, name))
	fields := strings.Fields(line)
	for i := 0; i < len(fields); i++ {
		if t := parseTimeString(fields[i]); !t.IsZero() {
			return t
		}
		if i+1 < len(fields) {
			if t := parseTimeString(fields[i] + " " + fields[i+1]); !t.IsZero() {
				return t
			}
		}
	}
	return parseBackupTimeFromName(name)
}

func parseBackupTimeFromName(name string) time.Time {
	for i := len(name) - len("20060102_150405"); i >= 0; i-- {
		if i+len("20060102_150405") > len(name) {
			continue
		}
		part := name[i : i+len("20060102_150405")]
		if part[8] != '_' {
			continue
		}
		if t, err := time.ParseInLocation("20060102_150405", part, time.Local); err == nil {
			return t
		}
	}
	return time.Time{}
}

func parseTimeString(s string) time.Time {
	s = strings.Trim(strings.TrimSpace(s), `"`)
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"20060102_150405",
	} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t
		}
	}
	return time.Time{}
}

func normalizeStatus(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch {
	case strings.Contains(s, "success"):
		return "success"
	case strings.Contains(s, "in progress"):
		return "in progress"
	case strings.Contains(s, "progress"):
		return "in progress"
	case strings.Contains(s, "running"):
		return "in progress"
	case strings.Contains(s, "error"):
		return "error"
	case strings.Contains(s, "fail"):
		return "failed"
	case strings.Contains(s, "done"):
		return "done"
	case s == "":
		return "unknown"
	default:
		return s
	}
}

func timeStamp() string {
	return time.Now().Format("2006-01-02 15:04:05")
}

func logInfo(format string, a ...interface{}) {
	fmt.Printf("[%s] [INFO] %s\n", timeStamp(), fmt.Sprintf(format, a...))
}
func logWarn(format string, a ...interface{}) {
	fmt.Printf("[%s] [WARN] %s\n", timeStamp(), fmt.Sprintf(format, a...))
}
func logError(format string, a ...interface{}) {
	fmt.Printf("[%s] [ERROR] %s\n", timeStamp(), fmt.Sprintf(format, a...))
}

func logInfoW(w io.Writer, format string, a ...interface{}) {
	fmt.Fprintf(w, "[%s] [INFO] %s\n", timeStamp(), fmt.Sprintf(format, a...))
}
func logWarnW(w io.Writer, format string, a ...interface{}) {
	fmt.Fprintf(w, "[%s] [WARN] %s\n", timeStamp(), fmt.Sprintf(format, a...))
}
func logErrorW(w io.Writer, format string, a ...interface{}) {
	fmt.Fprintf(w, "[%s] [ERROR] %s\n", timeStamp(), fmt.Sprintf(format, a...))
}
