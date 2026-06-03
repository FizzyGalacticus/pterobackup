package backup

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/fizzygalacticus/pterobackup/internal/domain"
)

var unsafeNameChars = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func backupBaseName(containerName, containerPath string) string {
	cleanContainer := sanitizeName(containerName)
	base := filepath.Base(strings.TrimRight(containerPath, "/"))
	cleanBase := sanitizeName(base)
	if cleanBase == "" || cleanBase == "." || cleanBase == "/" {
		cleanBase = "root"
	}

	ts := time.Now().UTC().Format("20060102T150405Z")

	return fmt.Sprintf("%s_%s_%s", cleanContainer, cleanBase, ts)
}

func sanitizeName(v string) string {
	v = unsafeNameChars.ReplaceAllString(v, "_")
	v = strings.Trim(v, "_")
	if v == "" {
		return "item"
	}

	return v
}

func backupRootDirectory() string {
	for _, value := range []string{
		os.Getenv("PTEROBACKUP_BACKUP_DIR"),
		os.Getenv("PTEROBACKUP_BACKUP_BASE_DIR"),
	} {
		if value != "" {
			return value
		}
	}

	return "./backups"
}

// BackupRootDirectory returns the resolved local backup root directory.
func BackupRootDirectory() string {
	return backupRootDirectory()
}

func backupSubdirectoryName(item domain.BackupItem) string {
	if item.BackupName != "" {
		return sanitizeName(item.BackupName)
	}

	if item.LocalTargetPath != "" {
		legacyName := strings.TrimRight(item.LocalTargetPath, "/")
		legacyName = filepath.Base(legacyName)
		if legacyName != "" && legacyName != "." && legacyName != "/" {
			return sanitizeName(legacyName)
		}
	}

	pathPart := strings.Trim(item.ContainerPath, "/")
	if pathPart == "" {
		pathPart = "root"
	}
	pathPart = strings.ReplaceAll(pathPart, "/", "__")

	return fmt.Sprintf("%s__%s", sanitizeName(item.ContainerName), sanitizeName(pathPart))
}

// BackupSubdirectoryName returns the per-item backup subdirectory name.
func BackupSubdirectoryName(item domain.BackupItem) string {
	return backupSubdirectoryName(item)
}

// TargetDirectoryForItem resolves the full local backup path for one item.
func TargetDirectoryForItem(item domain.BackupItem) string {
	return filepath.Join(backupRootDirectory(), backupSubdirectoryName(item))
}
