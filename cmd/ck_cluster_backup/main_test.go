package main

import "testing"

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
