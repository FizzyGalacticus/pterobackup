# PteroBackup — Agent Reference

Single-binary Go application with an embedded web UI that backs up files from running Docker containers on a remote host over SSH.

## What it does

- Connects to a remote host via SSH (password or private key auth).
- Runs `docker cp` on the remote host to extract files/directories from named containers.
- Downloads those files locally via SFTP.
- Stores backups with content-hash-based deduplication (skips if nothing changed).
- Directories are archived as `.tar.gz` on the remote before download.
- Prunes old artifacts automatically per item (`maxBackups`).
- Restores by uploading the latest artifact back to the remote and copying it into the container.
- Runs per-item scheduled backups in the background based on `intervalMinutes`.
- Serves a browser UI (static HTML/CSS/JS embedded in the binary) at `/`.

## Module

`github.com/fizzygalacticus/pterobackup` — Go 1.23+

## Package map

| Package | Path | Role |
|---|---|---|
| `main` | `cmd/pterobackup/` | Entry point. Wires store → server → scheduler → HTTP. |
| `domain` | `internal/domain/` | Plain types only: `AppConfig`, `SSHConfig`, `BackupItem`, `BackupOutcome`, `BackupRunResult`, `BackupScheduleStatus`. No logic. |
| `config` | `internal/config/` | Reads/writes `config.json` from disk. `Store` implements `configStore`. |
| `remote` | `internal/remote/` | SSH connection: `ssh_factory.go` builds the session, `executor.go` runs commands, `transfer.go` does SFTP up/down. |
| `backup` | `internal/backup/` | Core backup/restore logic. `Service` takes an `SSHSessionFactory` interface — all remote I/O goes through `RemoteExecutor` and `FileTransfer` interfaces (testable without SSH). Naming/hash helpers in `naming.go`. |
| `scheduler` | `internal/scheduler/` | Ticker-based scheduler. Checks due items every minute, calls `RunBackup` per due item, tracks `nextRunAt`/`lastSuccess`/`lastError` in memory. |
| `httpapi` | `internal/httpapi/` | `http.ServeMux` server. Handles all `/api/*` routes + serves the embedded static frontend. |
| `web` | `internal/web/` | `embed.go` exposes `web.Assets` (`embed.FS`) containing `web/index.html`, `web/app.js`, `web/style.css`. |

## Key interfaces (`internal/backup/interfaces.go`)

```go
RemoteExecutor  // Run(ctx, command) (stdout, stderr, error)
FileTransfer    // Download(ctx, remote, local) / Upload(ctx, local, remote)
SSHSessionFactory // Connect(ctx) → (RemoteExecutor, FileTransfer, io.Closer, error)
```

## HTTP API

| Method | Path | Notes |
|---|---|---|
| GET | `/api/config` | Load full config |
| PUT | `/api/config` | Save full config |
| POST | `/api/backup` | Run all items |
| POST | `/api/backup/item` | Run one item `{itemId}` |
| GET | `/api/backup/item/files?itemId=` | List local artifacts for an item |
| GET | `/api/backup/item/file?itemId=&name=` | Download a backup artifact (`Content-Disposition: attachment`) |
| DELETE | `/api/backup/item/file?itemId=&name=` | Delete a specific artifact |
| GET | `/api/backup/item/file/contents?itemId=&name=` | List file paths inside a `.tar.gz` artifact; returns `{"paths": [...]}` with `payload/` prefix stripped |
| POST | `/api/restore` | Restore all items |
| POST | `/api/restore/item` | Restore one item `{itemId}` |
| GET | `/api/schedule` | Scheduler status snapshot |
| GET | `/api/ssh/public-key` | Derive public key from stored private key |
| POST | `/api/ssh/generate-keypair` | Generate RSA 2048 keypair, return both halves |

## Config & storage

- Config JSON default: `~/.config/pterobackup/config.json`
- Overridden by env (priority order): `PTEROBACKUP_CONFIG_DIR` → `PTEROBACKUP_CONFIG_BASE_DIR` → `XDG_CONFIG_HOME`
- Backup artifact root default: `./backups`
- Overridden by: `PTEROBACKUP_BACKUP_DIR` → `PTEROBACKUP_BACKUP_BASE_DIR`
- Artifacts stored at: `<root>/<backupName or containerName+path>/<date>-<sha256hash>-<timestamp>[.tar.gz]`
- Hash is encoded in the filename; deduplication reads it back from the latest filename without a separate hash file.

## Domain types (summary)

```go
AppConfig        { SSH SSHConfig; Backups []BackupItem }
SSHConfig        { Host, Port, Username, AuthMethod, Password, PrivateKeyPath, PrivateKeyValue }
BackupItem       { ID, ContainerName, ContainerPath, BackupName, LocalTargetPath, IntervalMinutes, MaxBackups }
BackupOutcome    { ItemID, ArchivePath, IsCompressed, Skipped }
```

`BackupItem.ID` is auto-generated when missing. `BackupName` overrides the subdirectory name. `MaxBackups=0` disables pruning.

## Running

```bash
go run ./cmd/pterobackup          # dev, default :8080
go build -o pterobackup ./cmd/pterobackup

# Docker
docker build -t pterobackup:latest .
docker run --rm -p 8080:8080 -v "$PWD/config:/config" -v "$PWD/backups:/backups" pterobackup:latest
```

## Tests

```bash
go test ./...
```

Test files: `cmd/pterobackup/main_test.go`, `internal/backup/service_test.go`, `internal/config/store_test.go`, `internal/httpapi/server_ssh_test.go`, `internal/scheduler/scheduler_test.go`.

Architecture is designed for unit testing: backup logic accepts interface mocks instead of real SSH.

## Security posture (summary)

Designed for trusted private networks — **not hardened for public internet exposure.**

| Issue | Detail |
|---|---|
| No UI/API authentication | All endpoints are open to any client that can reach the port |
| No TLS | HTTP only; credentials travel unencrypted; use a TLS-terminating reverse proxy |
| SSH host key verification disabled | `ssh.InsecureIgnoreHostKey()` — MITM risk on untrusted networks |
| Plaintext credentials in config | SSH password or private key stored as unencrypted JSON (file written 0600) |
| `GET /api/config` returns credentials | No access control on the config endpoint |
| Backup files freely downloadable | No auth on download/contents endpoints |

**Mitigations in place:** config written `0600`; all file endpoints guard against path traversal; SFTP used for remote transfers.

**Recommended:** bind behind a reverse proxy with auth; firewall port 8080; use SSH key auth over password; dedicate a minimal-privilege SSH user on the remote host. See README for full details.
