package main

import (
	"strings"
	"testing"
)

func TestApplyBackupTypeDefaultsToFull(t *testing.T) {
	cfg := &Config{}
	if err := applyBackupType(cfg, ""); err != nil {
		t.Fatalf("applyBackupType returned error: %v", err)
	}
	if cfg.Backup.Type != "full" {
		t.Fatalf("type = %q, want full", cfg.Backup.Type)
	}
	if cfg.Backup.Upload.DiffFromRemote != "" {
		t.Fatalf("diff_from_remote = %q, want empty", cfg.Backup.Upload.DiffFromRemote)
	}
}

func TestApplyBackupTypeIncrDefaultsToLatestFullRemote(t *testing.T) {
	cfg := &Config{}
	if err := applyBackupType(cfg, "incr"); err != nil {
		t.Fatalf("applyBackupType returned error: %v", err)
	}
	if cfg.Backup.Type != "incr" {
		t.Fatalf("type = %q, want incr", cfg.Backup.Type)
	}
	if cfg.Backup.Upload.DiffFromRemote != "latest-full" {
		t.Fatalf("diff_from_remote = %q, want latest-full", cfg.Backup.Upload.DiffFromRemote)
	}
}

func TestApplyBackupTypeFullClearsDiffConfig(t *testing.T) {
	cfg := &Config{}
	cfg.Backup.Upload.DiffFromRemote = "ck_backup_20260722_175304"
	if err := applyBackupType(cfg, "full"); err != nil {
		t.Fatalf("applyBackupType returned error: %v", err)
	}
	if cfg.Backup.Type != "full" {
		t.Fatalf("type = %q, want full", cfg.Backup.Type)
	}
	if cfg.Backup.Upload.DiffFromRemote != "" {
		t.Fatalf("diff_from_remote = %q, want empty", cfg.Backup.Upload.DiffFromRemote)
	}
}

func TestApplyBackupTypeRejectsUnknownType(t *testing.T) {
	cfg := &Config{}
	if err := applyBackupType(cfg, "daily"); err == nil {
		t.Fatal("applyBackupType returned nil, want error")
	}
}

func TestSelectLatestRemoteBackupPrefersFullBackupForLatestFullMode(t *testing.T) {
	items := []backupListItem{
		{Name: "ck_backup_full_20260722_010000"},
		{Name: "ck_backup_incr_20260723_010000"},
		{Name: "ck_backup_full_20260721_010000"},
	}
	got := selectLatestRemoteBackup(items, "ck_backup", "", "latest-full")
	if got != "ck_backup_full_20260722_010000" {
		t.Fatalf("selected %q, want ck_backup_full_20260722_010000", got)
	}
}

func TestSelectLatestRemoteBackupSkipsBroken(t *testing.T) {
	items := []backupListItem{
		{Name: "ck_backup_full_20260722_010000", Broken: true},
		{Name: "ck_backup_full_20260721_010000"},
	}
	got := selectLatestRemoteBackup(items, "ck_backup", "", "latest-full")
	if got != "ck_backup_full_20260721_010000" {
		t.Fatalf("selected %q, want ck_backup_full_20260721_010000", got)
	}
}

func TestParseBackupListHandlesJSONStringItems(t *testing.T) {
	data := []byte(`[
		"{\"name\":\"ck_backup_full_20260803_081501\",\"created\":\"2026-08-03 10:03:51\"}",
		"{\"name\":\"ck_backup_incr_20260805_081501\",\"created\":\"2026-08-05 08:15:11\",\"broken\":true}"
	]`)

	items := parseBackupList(data)
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	if items[0].Name != "ck_backup_full_20260803_081501" {
		t.Fatalf("first item name = %q", items[0].Name)
	}
	if items[0].Created.IsZero() {
		t.Fatal("first item created time was not parsed")
	}
	if !items[1].Broken {
		t.Fatal("second item should be marked broken")
	}

	got := selectLatestRemoteBackup(items, "ck_backup", "", "latest-full")
	if got != "ck_backup_full_20260803_081501" {
		t.Fatalf("selected %q, want ck_backup_full_20260803_081501", got)
	}
}

func TestParseBackupListHandlesJSONLineWithCreatedSpace(t *testing.T) {
	data := []byte(`{"name":"ck_backup_full_20260803_081501","created":"2026-08-03 10:03:51"}` + "\n")

	items := parseBackupList(data)
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	if items[0].Name != "ck_backup_full_20260803_081501" {
		t.Fatalf("item name = %q, want ck_backup_full_20260803_081501", items[0].Name)
	}

	got := selectLatestRemoteBackup(items, "ck_backup", "", "latest-full")
	if got != "ck_backup_full_20260803_081501" {
		t.Fatalf("selected %q, want ck_backup_full_20260803_081501", got)
	}
}

func TestParseDiskUsage(t *testing.T) {
	data := []byte("Filesystem      Size  Used Avail Use% Mounted on\n/dev/sda        1.0T  375G  650G  37% /data\n")

	got := parseDiskUsage("/data/clickhouse/data", data)
	for _, want := range []string{
		"/data/clickhouse/data",
		"总量=1.0T",
		"已用=375G",
		"可用=650G",
		"使用率=37%",
		"挂载点=/data",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("parseDiskUsage() = %q, want it to contain %q", got, want)
		}
	}
}
