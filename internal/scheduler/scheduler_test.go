package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/FizzyGalacticus/pterobackup/internal/domain"
)

type fakeStore struct {
	cfg domain.AppConfig
}

func (f fakeStore) Load() (domain.AppConfig, error) {
	return f.cfg, nil
}

type fakeRunner struct {
	runs []domain.AppConfig
}

func (f *fakeRunner) RunBackup(_ context.Context, cfg domain.AppConfig) (domain.BackupRunResult, error) {
	f.runs = append(f.runs, cfg)
	return domain.BackupRunResult{}, nil
}

func TestRunOnceSchedulesOnlyDueItems(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{}
	svc := NewService(
		fakeStore{cfg: domain.AppConfig{
			SSH: domain.SSHConfig{Host: "127.0.0.1", Port: 22, Username: "u", AuthMethod: domain.AuthMethodPassword, Password: "p"},
			Backups: []domain.BackupItem{
				{ID: "a", ContainerName: "c1", ContainerPath: "/x", LocalTargetPath: "/tmp/a", IntervalMinutes: 60},
				{ID: "b", ContainerName: "c2", ContainerPath: "/y", LocalTargetPath: "/tmp/b", IntervalMinutes: 60},
			},
		}},
		func(_ domain.SSHConfig) BackupRunner { return runner },
		time.Minute,
	)
	service := svc
	service.logf = func(string, ...any) {}

	now := time.Now().UTC()
	service.nextRunByItem["a"] = now.Add(-time.Minute)
	service.nextRunByItem["b"] = now.Add(time.Hour)

	service.runOnce(context.Background())

	if len(runner.runs) != 1 {
		t.Fatalf("expected one run, got %d", len(runner.runs))
	}

	if len(runner.runs[0].Backups) != 1 {
		t.Fatalf("expected one due backup item, got %d", len(runner.runs[0].Backups))
	}

	if runner.runs[0].Backups[0].ID != "a" {
		t.Fatalf("expected item a, got %s", runner.runs[0].Backups[0].ID)
	}
}

func TestRunOnceInitializesNewItemsAsDue(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{}
	svc := NewService(
		fakeStore{cfg: domain.AppConfig{
			SSH:     domain.SSHConfig{Host: "127.0.0.1", Port: 22, Username: "u", AuthMethod: domain.AuthMethodPassword, Password: "p"},
			Backups: []domain.BackupItem{{ID: "a", ContainerName: "c1", ContainerPath: "/x", LocalTargetPath: "/tmp/a", IntervalMinutes: 30}},
		}},
		func(_ domain.SSHConfig) BackupRunner { return runner },
		time.Minute,
	)
	svc.logf = func(string, ...any) {}

	svc.runOnce(context.Background())

	if len(runner.runs) != 1 {
		t.Fatalf("expected initial run for new item, got %d", len(runner.runs))
	}

	next, ok := svc.nextRunByItem["a"]
	if !ok {
		t.Fatalf("expected next run tracking for item a")
	}

	if next.Before(time.Now().UTC().Add(29 * time.Minute)) {
		t.Fatalf("expected next run about 30 minutes out, got %s", next)
	}
}
