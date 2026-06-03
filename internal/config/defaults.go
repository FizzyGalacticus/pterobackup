package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/fizzygalacticus/pterobackup/internal/domain"
)

func DefaultConfig() domain.AppConfig {
	return domain.AppConfig{
		SSH: domain.SSHConfig{
			Port:       22,
			AuthMethod: domain.AuthMethodPassword,
		},
		Backups: []domain.BackupItem{},
	}
}

func applyDefaults(cfg *domain.AppConfig) {
	if cfg.SSH.Port == 0 {
		cfg.SSH.Port = 22
	}

	if cfg.SSH.AuthMethod == "" {
		cfg.SSH.AuthMethod = domain.AuthMethodPassword
	}

	if cfg.Backups == nil {
		cfg.Backups = []domain.BackupItem{}
	}

	usedIDs := make(map[string]struct{}, len(cfg.Backups))
	for i := range cfg.Backups {
		if cfg.Backups[i].ID == "" {
			continue
		}
		usedIDs[cfg.Backups[i].ID] = struct{}{}
	}

	for i := range cfg.Backups {
		if cfg.Backups[i].ID == "" {
			cfg.Backups[i].ID = nextBackupID(usedIDs)
		}

		if cfg.Backups[i].IntervalMinutes <= 0 {
			cfg.Backups[i].IntervalMinutes = domain.DefaultIntervalMin
		}

		if cfg.Backups[i].MaxBackups <= 0 {
			cfg.Backups[i].MaxBackups = domain.DefaultMaxBackups
		}
	}
}

func nextBackupID(used map[string]struct{}) string {
	for {
		candidate := generateBackupID()
		if _, exists := used[candidate]; exists {
			continue
		}

		used[candidate] = struct{}{}
		return candidate
	}
}

func generateBackupID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err == nil {
		return "item-" + hex.EncodeToString(b)
	}

	return fmt.Sprintf("item-%d", time.Now().UnixNano())
}
