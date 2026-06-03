package main

import (
	"path/filepath"
	"testing"
)

func TestDefaultConfigPathUsesPteroBackupConfigDir(t *testing.T) {
	t.Setenv("PTEROBACKUP_CONFIG_DIR", "/custom/config")
	t.Setenv("PTEROBACKUP_CONFIG_BASE_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", "")

	got := defaultConfigPath()
	want := filepath.Join("/custom/config", "config.json")
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestDefaultConfigPathUsesConfigBaseDirWhenPrimaryUnset(t *testing.T) {
	t.Setenv("PTEROBACKUP_CONFIG_DIR", "")
	t.Setenv("PTEROBACKUP_CONFIG_BASE_DIR", "/base/dir")
	t.Setenv("XDG_CONFIG_HOME", "")

	got := defaultConfigPath()
	want := filepath.Join("/base/dir", "config.json")
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestDefaultConfigPathUsesXDGConfigHome(t *testing.T) {
	t.Setenv("PTEROBACKUP_CONFIG_DIR", "")
	t.Setenv("PTEROBACKUP_CONFIG_BASE_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", "/xdg")

	got := defaultConfigPath()
	want := filepath.Join("/xdg", "pterobackup", "config.json")
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}
