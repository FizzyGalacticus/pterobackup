package scheduler

import (
	"context"
	"log"
	"sync"
	"time"

	"pterobackup/internal/domain"
)

type ConfigStore interface {
	Load() (domain.AppConfig, error)
}

type BackupRunner interface {
	RunBackup(ctx context.Context, cfg domain.AppConfig) (domain.BackupRunResult, error)
}

type RunnerFactory func(cfg domain.SSHConfig) BackupRunner

// Service periodically evaluates configured backup intervals and runs due jobs.
type Service struct {
	store         ConfigStore
	newRunner     RunnerFactory
	checkEvery    time.Duration
	logf          func(string, ...any)
	mu            sync.RWMutex
	nextRunByItem map[string]time.Time
	lastSuccess   map[string]time.Time
	lastError     map[string]string
}

func NewService(store ConfigStore, newRunner RunnerFactory, checkEvery time.Duration) *Service {
	if checkEvery <= 0 {
		checkEvery = time.Minute
	}

	return &Service{
		store:         store,
		newRunner:     newRunner,
		checkEvery:    checkEvery,
		logf:          log.Printf,
		nextRunByItem: map[string]time.Time{},
		lastSuccess:   map[string]time.Time{},
		lastError:     map[string]string{},
	}
}

func (s *Service) Snapshot() map[string]domain.BackupScheduleStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make(map[string]domain.BackupScheduleStatus, len(s.nextRunByItem))
	for itemID, next := range s.nextRunByItem {
		status := domain.BackupScheduleStatus{}
		if !next.IsZero() {
			status.NextRunAt = next.UTC().Format(time.RFC3339)
		}

		if last, ok := s.lastSuccess[itemID]; ok && !last.IsZero() {
			status.LastSuccessAt = last.UTC().Format(time.RFC3339)
		}

		if errText, ok := s.lastError[itemID]; ok {
			status.LastError = errText
		}

		out[itemID] = status
	}

	return out
}

func (s *Service) Start(ctx context.Context) {
	ticker := time.NewTicker(s.checkEvery)
	defer ticker.Stop()

	s.runOnce(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runOnce(ctx)
		}
	}
}

func (s *Service) runOnce(ctx context.Context) {
	cfg, err := s.store.Load()
	if err != nil {
		s.logf("scheduler load config: %v", err)
		return
	}

	now := time.Now().UTC()
	activeIDs := make(map[string]struct{}, len(cfg.Backups))
	dueItems := make([]domain.BackupItem, 0, len(cfg.Backups))

	s.mu.Lock()
	for _, item := range cfg.Backups {
		activeIDs[item.ID] = struct{}{}

		next, ok := s.nextRunByItem[item.ID]
		if !ok {
			s.nextRunByItem[item.ID] = now
			next = now
		}

		if now.Before(next) {
			continue
		}

		dueItems = append(dueItems, item)
	}

	for itemID := range s.nextRunByItem {
		if _, ok := activeIDs[itemID]; ok {
			continue
		}

		delete(s.nextRunByItem, itemID)
		delete(s.lastSuccess, itemID)
		delete(s.lastError, itemID)
	}
	s.mu.Unlock()

	if len(dueItems) == 0 {
		return
	}

	cfg.Backups = dueItems
	runner := s.newRunner(cfg.SSH)
	result, err := runner.RunBackup(ctx, cfg)
	if err != nil {
		s.mu.Lock()
		for _, item := range dueItems {
			s.lastError[item.ID] = err.Error()
		}
		s.mu.Unlock()

		s.logf("scheduler backup run failed for %d item(s): %v", len(dueItems), err)
		return
	}

	s.mu.Lock()
	for _, item := range dueItems {
		s.nextRunByItem[item.ID] = now.Add(time.Duration(item.IntervalMinutes) * time.Minute)
		s.lastSuccess[item.ID] = now
		delete(s.lastError, item.ID)
	}
	s.mu.Unlock()

	s.logf("scheduler backup run complete: %d result(s)", len(result.Results))
}
