package backup

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fizzygalacticus/pterobackup/internal/domain"
)

type fakeFactory struct {
	exec     RemoteExecutor
	transfer *fakeTransfer
	closer   fakeCloser
}

func (f fakeFactory) Connect(_ context.Context) (RemoteExecutor, FileTransfer, io.Closer, error) {
	return f.exec, f.transfer, f.closer, nil
}

type fakeCloser struct{}

func (fakeCloser) Close() error { return nil }

type fakeExecutor struct {
	commands []string
}

func (f *fakeExecutor) Run(_ context.Context, command string) (string, string, error) {
	f.commands = append(f.commands, command)
	if strings.Contains(command, "if [ -d") {
		if strings.Contains(command, "filemode") {
			return "file\n", "", nil
		}
		return "dir\n", "", nil
	}
	return "", "", nil
}

// hashingExecutor behaves like fakeExecutor but returns a configurable hash
// for sha256sum commands, enabling tests of the deduplication path.
type hashingExecutor struct {
	fakeExecutor
	hash string
}

func (h *hashingExecutor) Run(ctx context.Context, command string) (string, string, error) {
	if strings.Contains(command, "sha256sum") {
		h.commands = append(h.commands, command)
		return h.hash + "\n", "", nil
	}
	return h.fakeExecutor.Run(ctx, command)
}

type fakeTransfer struct {
	downloads [][2]string
	uploads   [][2]string
}

func (f *fakeTransfer) Download(_ context.Context, remotePath, localPath string) error {
	f.downloads = append(f.downloads, [2]string{remotePath, localPath})
	return os.WriteFile(localPath, []byte("ok"), 0o644)
}

func (f *fakeTransfer) Upload(_ context.Context, localPath, remotePath string) error {
	f.uploads = append(f.uploads, [2]string{localPath, remotePath})
	return nil
}

func TestRunBackupCompressesDirectory(t *testing.T) {
	exec := &fakeExecutor{}
	transfer := &fakeTransfer{}
	svc := NewService(fakeFactory{exec: exec, transfer: transfer, closer: fakeCloser{}})

	rootDir := t.TempDir()
	t.Setenv("PTEROBACKUP_BACKUP_DIR", rootDir)
	cfg := domain.AppConfig{
		Backups: []domain.BackupItem{{
			ID:            "abc",
			ContainerName: "panel",
			ContainerPath: "/var/lib/data",
			BackupName:    "panel-data",
			MaxBackups:    5,
		}},
	}

	result, err := svc.RunBackup(context.Background(), cfg)
	if err != nil {
		t.Fatalf("run backup: %v", err)
	}

	if len(result.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result.Results))
	}

	out := result.Results[0]
	if !out.IsCompressed {
		t.Fatalf("expected compressed result")
	}

	if filepath.Base(filepath.Dir(out.ArchivePath)) != "panel-data" {
		t.Fatalf("expected custom backup subdirectory panel-data, got %s", filepath.Base(filepath.Dir(out.ArchivePath)))
	}

	if !strings.HasSuffix(out.ArchivePath, ".tar.gz") {
		t.Fatalf("expected .tar.gz output, got %s", out.ArchivePath)
	}

	if _, err := os.Stat(out.ArchivePath); err != nil {
		t.Fatalf("stat output file: %v", err)
	}
}

func TestRunRestoreUploadsLatestBackup(t *testing.T) {
	exec := &fakeExecutor{}
	transfer := &fakeTransfer{}
	svc := NewService(fakeFactory{exec: exec, transfer: transfer, closer: fakeCloser{}})

	rootDir := t.TempDir()
	t.Setenv("PTEROBACKUP_BACKUP_DIR", rootDir)

	item := domain.BackupItem{
		ID:            "abc",
		ContainerName: "panel",
		ContainerPath: "/var/lib/data",
		BackupName:    "panel-data",
		MaxBackups:    5,
	}

	targetDir := filepath.Join(rootDir, "panel-data")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir targetDir: %v", err)
	}

	first := filepath.Join(targetDir, "20240101-abc123def456-000000Z.tar.gz")
	second := filepath.Join(targetDir, "20250101-xyz789def456-000000Z.tar.gz")
	if err := os.WriteFile(first, []byte("a"), 0o644); err != nil {
		t.Fatalf("write first: %v", err)
	}
	if err := os.WriteFile(second, []byte("b"), 0o644); err != nil {
		t.Fatalf("write second: %v", err)
	}

	cfg := domain.AppConfig{
		Backups: []domain.BackupItem{item},
	}

	if err := svc.RunRestore(context.Background(), cfg); err != nil {
		t.Fatalf("run restore: %v", err)
	}

	if len(transfer.uploads) != 1 {
		t.Fatalf("expected one upload, got %d", len(transfer.uploads))
	}

	if transfer.uploads[0][0] != second {
		t.Fatalf("expected latest backup %s, got %s", second, transfer.uploads[0][0])
	}
}

func TestRunBackupUsesDefaultPairSubdirectoryWhenBackupNameEmpty(t *testing.T) {
	exec := &fakeExecutor{}
	transfer := &fakeTransfer{}
	svc := NewService(fakeFactory{exec: exec, transfer: transfer, closer: fakeCloser{}})

	rootDir := t.TempDir()
	t.Setenv("PTEROBACKUP_BACKUP_DIR", rootDir)

	cfg := domain.AppConfig{
		Backups: []domain.BackupItem{{
			ID:            "abc",
			ContainerName: "panel",
			ContainerPath: "/var/lib/data",
			MaxBackups:    5,
		}},
	}

	result, err := svc.RunBackup(context.Background(), cfg)
	if err != nil {
		t.Fatalf("run backup: %v", err)
	}

	if len(result.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result.Results))
	}

	gotSubdir := filepath.Base(filepath.Dir(result.Results[0].ArchivePath))
	if gotSubdir != "panel__var__lib__data" {
		t.Fatalf("expected default subdir panel__var__lib__data, got %s", gotSubdir)
	}
}

func TestRunBackupPrunesOldBackupsByMaxBackups(t *testing.T) {
	exec := &fakeExecutor{}
	transfer := &fakeTransfer{}
	svc := NewService(fakeFactory{exec: exec, transfer: transfer, closer: fakeCloser{}})

	rootDir := t.TempDir()
	t.Setenv("PTEROBACKUP_BACKUP_DIR", rootDir)

	targetDir := filepath.Join(rootDir, "panel-data")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir targetDir: %v", err)
	}

	oldA := filepath.Join(targetDir, "old-a.tar.gz")
	oldB := filepath.Join(targetDir, "old-b.tar.gz")
	oldC := filepath.Join(targetDir, "old-c.tar.gz")
	for _, path := range []string{oldA, oldB, oldC} {
		if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
			t.Fatalf("write old file %s: %v", path, err)
		}
	}

	now := time.Now()
	_ = os.Chtimes(oldA, now.Add(-3*time.Hour), now.Add(-3*time.Hour))
	_ = os.Chtimes(oldB, now.Add(-2*time.Hour), now.Add(-2*time.Hour))
	_ = os.Chtimes(oldC, now.Add(-1*time.Hour), now.Add(-1*time.Hour))

	cfg := domain.AppConfig{
		Backups: []domain.BackupItem{{
			ID:            "abc",
			ContainerName: "panel",
			ContainerPath: "/var/lib/data",
			BackupName:    "panel-data",
			MaxBackups:    2,
		}},
	}

	if _, err := svc.RunBackup(context.Background(), cfg); err != nil {
		t.Fatalf("run backup: %v", err)
	}

	entries, err := os.ReadDir(targetDir)
	if err != nil {
		t.Fatalf("readdir targetDir: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 retained backups, got %d", len(entries))
	}
}

func TestRunBackupSkipsWhenContentUnchanged(t *testing.T) {
	const knownHash = "abc123def456abc123def456abc123def456abc123def456abc123def456abc1"

	exec := &hashingExecutor{hash: knownHash}
	transfer := &fakeTransfer{}
	svc := NewService(fakeFactory{exec: exec, transfer: transfer, closer: fakeCloser{}})

	rootDir := t.TempDir()
	t.Setenv("PTEROBACKUP_BACKUP_DIR", rootDir)

	cfg := domain.AppConfig{
		Backups: []domain.BackupItem{{
			ID:            "abc",
			ContainerName: "panel",
			ContainerPath: "/var/lib/data",
			BackupName:    "panel-data",
			MaxBackups:    5,
		}},
	}

	// First run — hash is new, backup should be downloaded.
	result, err := svc.RunBackup(context.Background(), cfg)
	if err != nil {
		t.Fatalf("first backup run: %v", err)
	}
	if len(result.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result.Results))
	}
	if result.Results[0].Skipped {
		t.Fatal("first run should not be skipped")
	}
	if len(transfer.downloads) != 1 {
		t.Fatalf("expected 1 download on first run, got %d", len(transfer.downloads))
	}

	// Second run — same hash, backup should be skipped.
	result, err = svc.RunBackup(context.Background(), cfg)
	if err != nil {
		t.Fatalf("second backup run: %v", err)
	}
	if len(result.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result.Results))
	}
	if !result.Results[0].Skipped {
		t.Fatal("second run should be skipped because content hash is unchanged")
	}
	if len(transfer.downloads) != 1 {
		t.Fatalf("expected no additional download on second run, got %d total", len(transfer.downloads))
	}
}
