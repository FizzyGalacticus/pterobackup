package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/fizzygalacticus/pterobackup/internal/domain"
)

// Store persists and loads application configuration as JSON.
type Store struct {
	path string
	mu   sync.RWMutex
}

func NewStore(path string) *Store {
	return &Store{path: path}
}

func (s *Store) Load() (domain.AppConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cfg := DefaultConfig()

	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}

		return domain.AppConfig{}, fmt.Errorf("read config: %w", err)
	}

	if len(data) == 0 {
		return cfg, nil
	}

	if err := json.Unmarshal(data, &cfg); err != nil {
		return domain.AppConfig{}, fmt.Errorf("decode config: %w", err)
	}

	applyDefaults(&cfg)

	return cfg, nil
}

func (s *Store) Save(cfg domain.AppConfig) error {
	applyDefaults(&cfg)

	if err := validate(cfg); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}

	if err := os.WriteFile(s.path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	return nil
}

func validate(cfg domain.AppConfig) error {
	if cfg.SSH.Host == "" {
		return errors.New("ssh host is required")
	}

	if cfg.SSH.Username == "" {
		return errors.New("ssh username is required")
	}

	if cfg.SSH.Port <= 0 {
		return errors.New("ssh port must be greater than zero")
	}

	switch cfg.SSH.AuthMethod {
	case domain.AuthMethodPassword:
		if cfg.SSH.Password == "" {
			return errors.New("password auth requires a password")
		}
	case domain.AuthMethodKey:
		if cfg.SSH.PrivateKeyPath == "" && cfg.SSH.PrivateKeyValue == "" {
			return errors.New("key auth requires privateKeyPath or privateKeyValue")
		}
	default:
		return errors.New("invalid ssh auth method")
	}

	seenIDs := make(map[string]struct{}, len(cfg.Backups))
	for i, item := range cfg.Backups {
		if item.ID == "" {
			return fmt.Errorf("backups[%d].id is required", i)
		}

		if _, exists := seenIDs[item.ID]; exists {
			return fmt.Errorf("backups[%d].id must be unique", i)
		}
		seenIDs[item.ID] = struct{}{}

		if item.ContainerName == "" {
			return fmt.Errorf("backups[%d].containerName is required", i)
		}

		if item.ContainerPath == "" {
			return fmt.Errorf("backups[%d].containerPath is required", i)
		}

		if item.IntervalMinutes <= 0 {
			return fmt.Errorf("backups[%d].intervalMinutes must be greater than zero", i)
		}

		if item.MaxBackups <= 0 {
			return fmt.Errorf("backups[%d].maxBackups must be greater than zero", i)
		}
	}

	return nil
}
