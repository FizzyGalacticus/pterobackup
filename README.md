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
- `GET /api/backup/item/file?itemId=...&name=...` (download artifact)
- `DELETE /api/backup/item/file?itemId=...&name=...`
- `GET /api/backup/item/file/contents?itemId=...&name=...`
- `POST /api/restore`
- `POST /api/restore/item` (body: `{ "itemId": "..." }`)
- `GET /api/schedule`
- `GET /api/ssh/public-key`
- `POST /api/ssh/generate-keypair`
- `POST /api/ssh/revoke-key`

## Security

> **This application is designed for use on trusted private networks. It is not hardened for exposure to the public internet.**

### Known risks

| Risk | Severity | Detail |
|---|---|---|
| No web UI authentication | **Critical** | Every API endpoint — including config read/write, backup download, and SSH key management — is accessible to anyone who can reach the port. |
| No HTTPS/TLS | **High** | All traffic is plain HTTP. SSH credentials (password or private key) returned by `GET /api/config` travel unencrypted over the network. |
| SSH host key verification disabled | **High** | The SSH client uses `InsecureIgnoreHostKey()`. A man-in-the-middle can intercept the connection and receive the SSH credentials being used for authentication. |
| SSH credentials stored in plaintext | **Medium** | `config.json` stores the SSH password or private key as unencrypted JSON. Anyone with read access to that file has full SSH access to the configured remote host. |
| `GET /api/config` returns all credentials | **Medium** | The full config — including the SSH password or raw private key — is returned by the config endpoint with no access control. |
| Backup files freely downloadable | **Medium** | Backup artifacts and their internal file listings are accessible to any client that can reach the port. Depending on what is backed up, this may expose sensitive application data. |
| Remote user requires Docker access | **Low** | The SSH user on the remote host must be able to run `docker cp`. Such users typically have broad container access beyond the specific paths being backed up. |

### Mitigations in place

- `config.json` is written with `0600` permissions (owner read/write only).
- All file-serving endpoints validate that the resolved path remains within the expected backup directory, preventing path traversal attacks.
- SSH file transfer uses SFTP (encrypted in transit between the application and the remote host).
- New SSH keypairs are generated with RSA 2048.

### Recommended deployment practices

- **Bind to localhost and use a reverse proxy** with authentication (e.g. Caddy or nginx with `basic_auth`) in front of port 8080 rather than exposing it directly.
- **Use a firewall** (e.g. `ufw`, Docker network policies, or a cloud security group) to restrict access to port 8080 to trusted IP addresses only.
- **Prefer SSH key auth over password auth.** Use the built-in keypair generator; the resulting private key never needs to leave the application.
- **Create a dedicated SSH user** on the remote host with the minimum Docker permissions required (e.g. membership in the `docker` group scoped to specific containers where possible).
- **Mount config and backup directories as Docker volumes** (shown in the Docker run example above) so credentials are not baked into the image and can be backed up independently.
