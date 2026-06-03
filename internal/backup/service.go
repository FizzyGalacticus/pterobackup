package backup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/FizzyGalacticus/pterobackup/internal/domain"
)

// Service performs backup and restore operations using an SSH connection.
type Service struct {
	factory SSHSessionFactory
}

func NewService(factory SSHSessionFactory) *Service {
	return &Service{factory: factory}
}

func (s *Service) RunBackup(ctx context.Context, cfg domain.AppConfig) (domain.BackupRunResult, error) {
	exec, transfer, closer, err := s.factory.Connect(ctx)
	if err != nil {
		return domain.BackupRunResult{}, fmt.Errorf("connect ssh: %w", err)
	}
	defer func() { _ = closer.Close() }()

	rootDir := backupRootDirectory()
	result := domain.BackupRunResult{Results: make([]domain.BackupOutcome, 0, len(cfg.Backups))}

	for _, item := range cfg.Backups {
		outcome, err := runSingleBackup(ctx, exec, transfer, rootDir, item)
		if err != nil {
			return domain.BackupRunResult{}, err
		}

		result.Results = append(result.Results, outcome)
	}

	return result, nil
}

func (s *Service) RunRestore(ctx context.Context, cfg domain.AppConfig) error {
	exec, transfer, closer, err := s.factory.Connect(ctx)
	if err != nil {
		return fmt.Errorf("connect ssh: %w", err)
	}
	defer func() { _ = closer.Close() }()

	rootDir := backupRootDirectory()
	for _, item := range cfg.Backups {
		if err := runSingleRestore(ctx, exec, transfer, rootDir, item); err != nil {
			return err
		}
	}

	return nil
}

func runSingleBackup(ctx context.Context, exec RemoteExecutor, transfer FileTransfer, rootDir string, item domain.BackupItem) (domain.BackupOutcome, error) {
	targetDir := filepath.Join(rootDir, backupSubdirectoryName(item))
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return domain.BackupOutcome{}, fmt.Errorf("create local target %s: %w", targetDir, err)
	}

	staging := fmt.Sprintf("/tmp/pterobackup-%s", sanitizeName(item.ID))
	if _, stderr, err := exec.Run(ctx, shellCommand("rm -rf %s && mkdir -p %s", staging, staging)); err != nil {
		return domain.BackupOutcome{}, remoteErr("create staging", err, stderr)
	}

	remotePayload := filepath.ToSlash(filepath.Join(staging, "payload"))
	cpCmd := shellCommand("docker cp %s:%s %s", shellEscape(item.ContainerName), shellEscape(item.ContainerPath), shellEscape(remotePayload))
	if _, stderr, err := exec.Run(ctx, cpCmd); err != nil {
		return domain.BackupOutcome{}, remoteErr("docker cp from container", err, stderr)
	}

	isDirCmd := shellCommand("if [ -d %s ]; then echo dir; else echo file; fi", shellEscape(remotePayload))
	stdout, stderr, err := exec.Run(ctx, isDirCmd)
	if err != nil {
		return domain.BackupOutcome{}, remoteErr("detect payload type", err, stderr)
	}

	payloadType := strings.TrimSpace(stdout)
	baseName := backupBaseName(item.ContainerName, item.ContainerPath)
	localPath := filepath.Join(targetDir, baseName)
	remoteDownloadPath := remotePayload
	isCompressed := false

	if payloadType == "dir" {
		isCompressed = true
		localPath += ".tar.gz"
		remoteDownloadPath = filepath.ToSlash(filepath.Join(staging, "payload.tar.gz"))

		tarCmd := shellCommand("tar -C %s -czf %s payload", shellEscape(staging), shellEscape(remoteDownloadPath))
		if _, stderr, err := exec.Run(ctx, tarCmd); err != nil {
			return domain.BackupOutcome{}, remoteErr("create archive", err, stderr)
		}
	}

	if err := transfer.Download(ctx, remoteDownloadPath, localPath); err != nil {
		return domain.BackupOutcome{}, fmt.Errorf("download backup to %s: %w", localPath, err)
	}

	if err := pruneBackups(targetDir, item.MaxBackups); err != nil {
		return domain.BackupOutcome{}, fmt.Errorf("prune backups in %s: %w", targetDir, err)
	}

	_, _, _ = exec.Run(ctx, shellCommand("rm -rf %s", staging))

	return domain.BackupOutcome{
		ItemID:       item.ID,
		ArchivePath:  localPath,
		IsCompressed: isCompressed,
	}, nil
}

func runSingleRestore(ctx context.Context, exec RemoteExecutor, transfer FileTransfer, rootDir string, item domain.BackupItem) error {
	backupPath, compressed, err := latestBackupForItem(rootDir, item)
	if err != nil {
		return err
	}

	staging := fmt.Sprintf("/tmp/pterobackup-restore-%s", sanitizeName(item.ID))
	if _, stderr, err := exec.Run(ctx, shellCommand("rm -rf %s && mkdir -p %s", staging, staging)); err != nil {
		return remoteErr("create restore staging", err, stderr)
	}

	remoteUploaded := filepath.ToSlash(filepath.Join(staging, filepath.Base(backupPath)))
	if err := transfer.Upload(ctx, backupPath, remoteUploaded); err != nil {
		return fmt.Errorf("upload backup %s: %w", backupPath, err)
	}

	if compressed {
		extracted := filepath.ToSlash(filepath.Join(staging, "payload"))
		if _, stderr, err := exec.Run(ctx, shellCommand("mkdir -p %s && tar -C %s -xzf %s", shellEscape(extracted), shellEscape(extracted), shellEscape(remoteUploaded))); err != nil {
			return remoteErr("extract archive", err, stderr)
		}

		restoreCmd := shellCommand("docker cp %s/. %s:%s", shellEscape(filepath.ToSlash(filepath.Join(extracted, "payload"))), shellEscape(item.ContainerName), shellEscape(item.ContainerPath))
		if _, stderr, err := exec.Run(ctx, restoreCmd); err != nil {
			return remoteErr("docker cp restore directory", err, stderr)
		}
	} else {
		restoreCmd := shellCommand("docker cp %s %s:%s", shellEscape(remoteUploaded), shellEscape(item.ContainerName), shellEscape(item.ContainerPath))
		if _, stderr, err := exec.Run(ctx, restoreCmd); err != nil {
			return remoteErr("docker cp restore file", err, stderr)
		}
	}

	_, _, _ = exec.Run(ctx, shellCommand("rm -rf %s", staging))

	return nil
}

func latestBackupForItem(rootDir string, item domain.BackupItem) (path string, compressed bool, err error) {
	targetDir := filepath.Join(rootDir, backupSubdirectoryName(item))
	entries, err := os.ReadDir(targetDir)
	if err != nil {
		return "", false, fmt.Errorf("read local target path %s: %w", targetDir, err)
	}

	prefix := fmt.Sprintf("%s_%s_", sanitizeName(item.ContainerName), sanitizeName(filepath.Base(strings.TrimRight(item.ContainerPath, "/"))))
	latestName := ""
	for _, e := range entries {
		if e.IsDir() {
			continue
		}

		name := e.Name()
		if !strings.HasPrefix(name, prefix) {
			continue
		}

		if name > latestName {
			latestName = name
		}
	}

	if latestName == "" {
		return "", false, fmt.Errorf("no backup file found for item %s in %s", item.ID, targetDir)
	}

	fullPath := filepath.Join(targetDir, latestName)

	return fullPath, strings.HasSuffix(latestName, ".tar.gz"), nil
}

func shellEscape(v string) string {
	return "'" + strings.ReplaceAll(v, "'", "'\\''") + "'"
}

func shellCommand(format string, args ...any) string {
	return fmt.Sprintf(format, args...)
}

func remoteErr(operation string, err error, stderr string) error {
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return fmt.Errorf("%s: %w", operation, err)
	}

	return fmt.Errorf("%s: %w: %s", operation, err, stderr)
}

type backupFile struct {
	name    string
	modTime time.Time
}

func pruneBackups(targetDir string, maxBackups int) error {
	if maxBackups <= 0 {
		return nil
	}

	entries, err := os.ReadDir(targetDir)
	if err != nil {
		return err
	}

	files := make([]backupFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("stat %s: %w", entry.Name(), err)
		}

		files = append(files, backupFile{name: entry.Name(), modTime: info.ModTime()})
	}

	if len(files) <= maxBackups {
		return nil
	}

	sort.Slice(files, func(i, j int) bool {
		if files[i].modTime.Equal(files[j].modTime) {
			return files[i].name < files[j].name
		}
		return files[i].modTime.Before(files[j].modTime)
	})

	toDelete := len(files) - maxBackups
	for i := 0; i < toDelete; i++ {
		path := filepath.Join(targetDir, files[i].name)
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove %s: %w", path, err)
		}
	}

	return nil
}
