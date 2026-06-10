package httpapi

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/fizzygalacticus/pterobackup/internal/backup"
	"github.com/fizzygalacticus/pterobackup/internal/config"
	"github.com/fizzygalacticus/pterobackup/internal/domain"
	"github.com/fizzygalacticus/pterobackup/internal/remote"
	"github.com/fizzygalacticus/pterobackup/internal/web"

	"golang.org/x/crypto/ssh"
)

type configStore interface {
	Load() (domain.AppConfig, error)
	Save(cfg domain.AppConfig) error
}

type backupRunner interface {
	RunBackup(ctx context.Context, cfg domain.AppConfig) (domain.BackupRunResult, error)
	RunRestore(ctx context.Context, cfg domain.AppConfig) error
}

type runnerFactory func(cfg domain.SSHConfig) backupRunner

type sshConnectFunc func(ctx context.Context, cfg domain.SSHConfig) (backup.RemoteExecutor, io.Closer, error)

type scheduleReader interface {
	Snapshot() map[string]domain.BackupScheduleStatus
}

type Server struct {
	store         configStore
	newRunner     runnerFactory
	sshConnect    sshConnectFunc
	schedule      scheduleReader
	serveStaticFS http.Handler
}

func NewServer(store configStore) (*Server, error) {
	sub, err := fs.Sub(web.Assets, "web")
	if err != nil {
		return nil, fmt.Errorf("open embedded web assets: %w", err)
	}

	server := &Server{
		store:         store,
		newRunner:     defaultRunnerFactory,
		sshConnect:    defaultSSHConnect,
		serveStaticFS: http.FileServer(http.FS(sub)),
	}

	return server, nil
}

func defaultRunnerFactory(sshCfg domain.SSHConfig) backupRunner {
	factory := remote.NewSSHFactory(sshCfg)
	return backup.NewService(factory)
}

func defaultSSHConnect(ctx context.Context, cfg domain.SSHConfig) (backup.RemoteExecutor, io.Closer, error) {
	exec, _, closer, err := remote.NewSSHFactory(cfg).Connect(ctx)
	return exec, closer, err
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/backup", s.handleBackup)
	mux.HandleFunc("/api/backup/item", s.handleBackupItem)
	mux.HandleFunc("/api/backup/item/files", s.handleBackupItemFiles)
	mux.HandleFunc("/api/backup/item/file/contents", s.handleBackupFileContents)
	mux.HandleFunc("/api/backup/item/file", s.handleBackupItemFile)
	mux.HandleFunc("/api/restore", s.handleRestore)
	mux.HandleFunc("/api/restore/item", s.handleRestoreItem)
	mux.HandleFunc("/api/schedule", s.handleSchedule)
	mux.HandleFunc("/api/ssh/public-key", s.handleSSHPublicKey)
	mux.HandleFunc("/api/ssh/generate-keypair", s.handleSSHGenerateKeypair)
	mux.HandleFunc("/api/ssh/revoke-key", s.handleSSHRevokeKey)
	mux.Handle("/", s.serveStaticFS)

	return withJSONError(mux)
}

func (s *Server) SetScheduleReader(reader scheduleReader) {
	s.schedule = reader
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg, err := s.store.Load()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "load config", err)
			return
		}

		writeJSON(w, http.StatusOK, cfg)
	case http.MethodPut:
		var cfg domain.AppConfig
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			writeErr(w, http.StatusBadRequest, "decode json", err)
			return
		}

		if err := s.store.Save(cfg); err != nil {
			writeErr(w, http.StatusBadRequest, "save config", err)
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	cfg, err := s.store.Load()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "load config", err)
		return
	}

	runner := s.newRunner(cfg.SSH)
	result, err := runner.RunBackup(r.Context(), cfg)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "run backup", err)
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	cfg, err := s.store.Load()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "load config", err)
		return
	}

	runner := s.newRunner(cfg.SSH)
	if err := runner.RunRestore(r.Context(), cfg); err != nil {
		writeErr(w, http.StatusInternalServerError, "run restore", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleBackupItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	cfg, err := s.store.Load()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "load config", err)
		return
	}

	itemCfg, err := singleItemConfig(r, cfg)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "select backup item", err)
		return
	}

	runner := s.newRunner(cfg.SSH)
	result, err := runner.RunBackup(r.Context(), itemCfg)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "run backup", err)
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleBackupItemFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	itemID := strings.TrimSpace(r.URL.Query().Get("itemId"))
	if itemID == "" {
		writeErr(w, http.StatusBadRequest, "itemId", errors.New("itemId is required"))
		return
	}

	cfg, err := s.store.Load()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "load config", err)
		return
	}

	item, err := findItemByID(cfg, itemID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "find backup item", err)
		return
	}

	artifacts, err := listBackupArtifacts(*item)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "list backup artifacts", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": artifacts})
}

// resolveBackupFilePath validates query params and returns the absolute local path
// of a backup file, guarding against path traversal.
func resolveBackupFilePath(r *http.Request, cfg domain.AppConfig) (filePath, fileName string, err error) {
	itemID := strings.TrimSpace(r.URL.Query().Get("itemId"))
	fileName = strings.TrimSpace(r.URL.Query().Get("name"))
	if itemID == "" || fileName == "" {
		return "", "", errors.New("itemId and name are required")
	}
	if strings.ContainsAny(fileName, "/\\") || fileName == ".." {
		return "", "", errors.New("invalid file name")
	}
	item, err := findItemByID(cfg, itemID)
	if err != nil {
		return "", "", fmt.Errorf("find backup item: %w", err)
	}
	targetDir := backup.TargetDirectoryForItem(*item)
	resolved := filepath.Join(targetDir, fileName)
	if !strings.HasPrefix(resolved, targetDir+string(os.PathSeparator)) {
		return "", "", errors.New("invalid file name")
	}
	return resolved, fileName, nil
}

func (s *Server) handleBackupItemFile(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleDownloadBackupFile(w, r)
	case http.MethodDelete:
		s.handleDeleteBackupFile(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleDownloadBackupFile(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.store.Load()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "load config", err)
		return
	}
	filePath, fileName, err := resolveBackupFilePath(r, cfg)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "resolve file", err)
		return
	}
	f, err := os.Open(filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeErr(w, http.StatusNotFound, "open file", err)
			return
		}
		writeErr(w, http.StatusInternalServerError, "open file", err)
		return
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "stat file", err)
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", fileName))
	http.ServeContent(w, r, fileName, stat.ModTime(), f)
}

func (s *Server) handleDeleteBackupFile(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.store.Load()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "load config", err)
		return
	}
	filePath, _, err := resolveBackupFilePath(r, cfg)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "resolve file", err)
		return
	}
	if err := os.Remove(filePath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeErr(w, http.StatusNotFound, "delete file", err)
			return
		}
		writeErr(w, http.StatusInternalServerError, "delete file", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleBackupFileContents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	cfg, err := s.store.Load()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "load config", err)
		return
	}
	filePath, _, err := resolveBackupFilePath(r, cfg)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "resolve file", err)
		return
	}
	paths, err := listTarContents(filePath)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "read archive", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"paths": paths})
}

func listTarContents(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var paths []string
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if hdr.Typeflag != tar.TypeDir {
			paths = append(paths, hdr.Name)
		}
	}
	return paths, nil
}

func (s *Server) handleRestoreItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	cfg, err := s.store.Load()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "load config", err)
		return
	}

	itemCfg, err := singleItemConfig(r, cfg)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "select backup item", err)
		return
	}

	runner := s.newRunner(cfg.SSH)
	if err := runner.RunRestore(r.Context(), itemCfg); err != nil {
		writeErr(w, http.StatusInternalServerError, "run restore", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type itemRequest struct {
	ItemID string `json:"itemId"`
}

func singleItemConfig(r *http.Request, cfg domain.AppConfig) (domain.AppConfig, error) {
	var req itemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return domain.AppConfig{}, fmt.Errorf("decode json: %w", err)
	}

	if req.ItemID == "" {
		return domain.AppConfig{}, errors.New("itemId is required")
	}

	item, err := findItemByID(cfg, req.ItemID)
	if err != nil {
		return domain.AppConfig{}, err
	}

	return domain.AppConfig{SSH: cfg.SSH, Backups: []domain.BackupItem{*item}}, nil
}

func findItemByID(cfg domain.AppConfig, itemID string) (*domain.BackupItem, error) {
	for i := range cfg.Backups {
		if cfg.Backups[i].ID != itemID {
			continue
		}

		return &cfg.Backups[i], nil
	}

	return nil, fmt.Errorf("itemId %q not found", itemID)
}

type backupArtifact struct {
	Name      string  `json:"name"`
	SizeBytes int64   `json:"sizeBytes"`
	SizeMB    float64 `json:"sizeMB"`
	Modified  string  `json:"modified"`
}

func listBackupArtifacts(item domain.BackupItem) ([]backupArtifact, error) {
	targetDir := backup.TargetDirectoryForItem(item)
	entries, err := os.ReadDir(targetDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []backupArtifact{}, nil
		}
		return nil, fmt.Errorf("read %s: %w", targetDir, err)
	}

	type withTime struct {
		artifact backupArtifact
		modTime  time.Time
	}
	items := make([]withTime, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", entry.Name(), err)
		}

		sizeBytes := info.Size()
		items = append(items, withTime{
			artifact: backupArtifact{
				Name:      entry.Name(),
				SizeBytes: sizeBytes,
				SizeMB:    float64(sizeBytes) / (1024 * 1024),
				Modified:  info.ModTime().UTC().Format(time.RFC3339),
			},
			modTime: info.ModTime(),
		})
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].modTime.Equal(items[j].modTime) {
			return items[i].artifact.Name > items[j].artifact.Name
		}
		return items[i].modTime.After(items[j].modTime)
	})

	out := make([]backupArtifact, 0, len(items))
	for _, item := range items {
		out = append(out, item.artifact)
	}

	return out, nil
}

func (s *Server) handleSchedule(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	if s.schedule == nil {
		writeJSON(w, http.StatusOK, map[string]any{"items": map[string]domain.BackupScheduleStatus{}})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": s.schedule.Snapshot()})
}

func (s *Server) handleSSHPublicKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	cfg, err := s.store.Load()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "load config", err)
		return
	}

	publicKey, hasKey, err := publicKeyFromSSHConfig(cfg.SSH)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "derive public key", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"hasKey":    hasKey,
		"publicKey": publicKey,
	})
}

func (s *Server) handleSSHGenerateKeypair(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	privateKey, publicKey, err := generateRSAKeypair()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "generate keypair", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"privateKey": privateKey,
		"publicKey":  publicKey,
	})
}

func (s *Server) handleSSHRevokeKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	cfg, err := s.store.Load()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "load config", err)
		return
	}

	pubKey, hasKey, err := publicKeyFromSSHConfig(cfg.SSH)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "derive public key", err)
		return
	}
	if !hasKey {
		writeErr(w, http.StatusBadRequest, "revoke key", errors.New("no SSH key configured"))
		return
	}

	// The authorized_keys line is "type blob [comment]"; blob is base64 (no shell-special chars).
	fields := strings.Fields(pubKey)
	if len(fields) < 2 {
		writeErr(w, http.StatusInternalServerError, "parse public key", errors.New("unexpected key format"))
		return
	}
	keyBlob := fields[1]

	exec, closer, err := s.sshConnect(r.Context(), cfg.SSH)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "connect ssh", err)
		return
	}
	defer closer.Close()

	// Use awk field comparison (not regex) so special chars in the blob are safe.
	removeCmd := fmt.Sprintf(
		"if [ -f ~/.ssh/authorized_keys ]; then awk -v k='%s' '$2 != k' ~/.ssh/authorized_keys > /tmp/.pterobackup_ak && mv /tmp/.pterobackup_ak ~/.ssh/authorized_keys; fi",
		keyBlob,
	)
	if _, stderr, err := exec.Run(r.Context(), removeCmd); err != nil {
		writeErr(w, http.StatusInternalServerError, fmt.Sprintf("remove from authorized_keys: %s", strings.TrimSpace(stderr)), err)
		return
	}

	newPrivateKey, newPublicKey, err := generateRSAKeypair()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "generate new keypair", err)
		return
	}

	cfg.SSH.PrivateKeyValue = newPrivateKey
	cfg.SSH.PrivateKeyPath = ""
	cfg.SSH.AuthMethod = domain.AuthMethodKey
	if err := s.store.Save(cfg); err != nil {
		writeErr(w, http.StatusInternalServerError, "save config", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "publicKey": newPublicKey})
}

func publicKeyFromSSHConfig(cfg domain.SSHConfig) (publicKey string, hasKey bool, err error) {
	privateKey := strings.TrimSpace(cfg.PrivateKeyValue)
	if privateKey == "" && cfg.PrivateKeyPath != "" {
		data, readErr := os.ReadFile(cfg.PrivateKeyPath)
		if readErr != nil {
			return "", true, fmt.Errorf("read private key path: %w", readErr)
		}
		privateKey = strings.TrimSpace(string(data))
	}

	if privateKey == "" {
		return "", false, nil
	}

	signer, parseErr := ssh.ParsePrivateKey([]byte(privateKey))
	if parseErr != nil {
		return "", true, fmt.Errorf("parse private key: %w", parseErr)
	}

	pub := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))

	return pub, true, nil
}

func generateRSAKeypair() (privateKeyPEM string, publicKey string, err error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", fmt.Errorf("generate rsa key: %w", err)
	}

	privateBlock := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	}
	privateKeyPEM = strings.TrimSpace(string(pem.EncodeToMemory(privateBlock)))

	pubKey, err := ssh.NewPublicKey(&key.PublicKey)
	if err != nil {
		return "", "", fmt.Errorf("build public key: %w", err)
	}

	publicKey = strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pubKey)))

	return privateKeyPEM, publicKey, nil
}

func withJSONError(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeErr(w http.ResponseWriter, status int, op string, err error) {
	writeJSON(w, status, map[string]string{"error": fmt.Sprintf("%s: %s", op, err.Error())})
}

func NewDefaultStore(configPath string) *config.Store {
	return config.NewStore(configPath)
}
