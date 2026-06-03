# PteroBackup

PteroBackup is a single-binary Go application with an embedded web UI to configure and run backups from files inside running Docker containers on a remote host over SSH.

## Features

- SSH using password or private key auth.
- Private keys can be pasted or uploaded in the web UI (stored in config as key content).
- SSH keypairs can be generated directly from the web UI.
- Derived SSH public key can be copied from the UI for host `authorized_keys` setup.
- JSON configuration persisted locally.
- Multiple backup items (container + path + local destination).
- Per-item backup retention (`maxBackups`) to cap disk growth.
- Directory paths are automatically archived as `.tar.gz`.
- Restore operation uploads latest backup artifact and copies back into the container.
- Static HTML/CSS/JS frontend embedded directly into the binary.
- Unit-testable architecture via interfaces for remote execution and file transfer.

## Run locally

```bash
go mod tidy
go run ./cmd/pterobackup
```

By default, config is stored at `~/.config/pterobackup/config.json`.

You can override the config base directory at runtime with environment variables:

- `PTEROBACKUP_CONFIG_DIR` (highest priority)
- `PTEROBACKUP_CONFIG_BASE_DIR`
- `XDG_CONFIG_HOME` (uses `$XDG_CONFIG_HOME/pterobackup/config.json`)

Backup artifact root directory can also be configured at runtime:

- `PTEROBACKUP_BACKUP_DIR` (highest priority)
- `PTEROBACKUP_BACKUP_BASE_DIR`

Default backup root when unset: `./backups`

Backups are stored in subdirectories beneath the backup root. By default, the subdirectory is derived from the `containerName` + `containerPath` pair. You can set an optional `backupName` per item to override that subdirectory name.
Each backup item also has a configurable `maxBackups` limit; older artifacts are pruned automatically after successful backups.

Backup item IDs are internal and auto-generated when missing.

## Docker build

```bash
docker build -t pterobackup:latest .
docker run --rm -p 8080:8080 -v "$PWD/config:/config" -v "$PWD/backups:/backups" pterobackup:latest
```

Inside the container, the default config path is `/config/config.json` via `PTEROBACKUP_CONFIG_DIR=/config`.
Inside the container, the default backup root path is `/backups` via `PTEROBACKUP_BACKUP_DIR=/backups`.

## HTTP API

- `GET /api/config`
- `PUT /api/config`
- `POST /api/backup`
- `POST /api/backup/item` (body: `{ "itemId": "..." }`)
- `GET /api/backup/item/files?itemId=...`
- `POST /api/restore`
- `POST /api/restore/item` (body: `{ "itemId": "..." }`)
- `GET /api/ssh/public-key`
- `POST /api/ssh/generate-keypair`

## Security notes

- SSH host key verification is currently disabled to keep the implementation simple.
- Use network controls and trusted hosts; tighten this in production environments.
