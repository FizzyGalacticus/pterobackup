package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"pterobackup/internal/backup"
	"pterobackup/internal/domain"
	"pterobackup/internal/httpapi"
	"pterobackup/internal/remote"
	"pterobackup/internal/scheduler"
)

func main() {
	var (
		addr       = flag.String("addr", ":8080", "HTTP bind address")
		configPath = flag.String("config", defaultConfigPath(), "Path to config JSON")
	)
	flag.Parse()

	store := httpapi.NewDefaultStore(*configPath)
	server, err := httpapi.NewServer(store)
	if err != nil {
		log.Fatalf("create server: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sched := scheduler.NewService(store, func(cfg domain.SSHConfig) scheduler.BackupRunner {
		return backup.NewService(remote.NewSSHFactory(cfg))
	}, time.Minute)
	server.SetScheduleReader(sched)
	go sched.Start(ctx)

	httpServer := &http.Server{
		Addr:              *addr,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	fmt.Printf("pterobackup listening on %s\n", *addr)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("listen: %v", err)
	}
}

func defaultConfigPath() string {
	if baseDir := firstNonEmpty(
		os.Getenv("PTEROBACKUP_CONFIG_DIR"),
		os.Getenv("PTEROBACKUP_CONFIG_BASE_DIR"),
		os.Getenv("XDG_CONFIG_HOME"),
	); baseDir != "" {
		if baseDir == os.Getenv("XDG_CONFIG_HOME") {
			return filepath.Join(baseDir, "pterobackup", "config.json")
		}

		return filepath.Join(baseDir, "config.json")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "./config.json"
	}

	return filepath.Join(home, ".config", "pterobackup", "config.json")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}

	return ""
}
