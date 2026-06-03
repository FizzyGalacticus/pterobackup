package config

import (
	"path/filepath"
	"testing"

	"github.com/fizzygalacticus/pterobackup/internal/domain"
)

func TestStoreLoadMissingReturnsDefaults(t *testing.T) {
	t.Parallel()

	store := NewStore(filepath.Join(t.TempDir(), "config.json"))

	cfg, err := store.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if cfg.SSH.Port != 22 {
		t.Fatalf("expected default port 22, got %d", cfg.SSH.Port)
	}

	if cfg.Backups == nil {
		t.Fatalf("expected non-nil backups slice")
	}
}

func TestStoreSaveAndLoadRoundTrip(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app", "config.json")
	store := NewStore(path)

	cfg := domain.AppConfig{
		SSH: domain.SSHConfig{
			Host:       "192.168.1.10",
			Port:       22,
			Username:   "backup",
			AuthMethod: domain.AuthMethodPassword,
			Password:   "secret",
		},
		Backups: []domain.BackupItem{{
			ID:              "1",
			ContainerName:   "panel",
			ContainerPath:   "/var/lib/panel",
			LocalTargetPath: "/tmp/backups",
		}},
	}

	if err := store.Save(cfg); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if loaded.SSH.Host != cfg.SSH.Host {
		t.Fatalf("host mismatch: got %q want %q", loaded.SSH.Host, cfg.SSH.Host)
	}

	if len(loaded.Backups) != 1 {
		t.Fatalf("expected 1 backup item, got %d", len(loaded.Backups))
	}

	if loaded.Backups[0].IntervalMinutes != domain.DefaultIntervalMin {
		t.Fatalf("expected default interval %d, got %d", domain.DefaultIntervalMin, loaded.Backups[0].IntervalMinutes)
	}
}

func TestStoreSaveGeneratesMissingBackupID(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app", "config.json")
	store := NewStore(path)

	cfg := domain.AppConfig{
		SSH: domain.SSHConfig{
			Host:       "192.168.1.10",
			Port:       22,
			Username:   "backup",
			AuthMethod: domain.AuthMethodPassword,
			Password:   "secret",
		},
		Backups: []domain.BackupItem{{
			ContainerName: "panel",
			ContainerPath: "/var/lib/panel",
		}},
	}

	if err := store.Save(cfg); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if len(loaded.Backups) != 1 {
		t.Fatalf("expected 1 backup item, got %d", len(loaded.Backups))
	}

	if loaded.Backups[0].ID == "" {
		t.Fatalf("expected generated backup ID")
	}
}
