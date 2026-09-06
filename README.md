# WebSSH Gateway

[中文文档](README_CN.md) · English

A lightweight, self-hosted Web SSH gateway for trusted LANs and VPNs. It ships as a single Go binary with an embedded xterm.js frontend and stores its data in SQLite.

WebSSH Gateway provides real interactive PTY sessions for tmux, Vim, htop, less, mouse reporting, alternate screens, function keys, resize events, and bracketed paste. It also includes encrypted credential storage, strict SSH host-key pinning, multi-tab terminals, mobile helper keys, UTF-8/GB18030 switching, audit events, and a responsive light/dark interface.

## Highlights

- Real SSH sessions using `golang.org/x/crypto/ssh`, `RequestPty`, and `WindowChange`
- Raw binary WebSocket terminal stream with a versioned JSON control channel
- Password and OpenSSH/PEM private-key authentication, including key passphrases
- AES-256-GCM encryption for stored SSH secrets; bcrypt for login passwords
- Trust on first use followed by strict SSH host-key pinning
- Host, group, tag, favorite, credential, settings, and audit management
- Multiple isolated terminal tabs with search and clipboard controls
- iPad/mobile toolbar for Esc, Tab, Ctrl, Alt, arrows, Page Up/Down, and paste
- Live UTF-8 and GB18030 terminal encoding selection
- Persistent dark and light themes, including matching terminal palettes
- SQLite WAL, CSRF and Origin validation, login throttling, CSP, and session limits
- Docker, systemd, health checks, backup command, CI, and automated releases

## Quick start

Download a binary from [GitHub Releases](https://github.com/zhangchaosd/WebSSH-Gateway/releases), or build locally:

```bash
npm ci
npm run build
go build -trimpath -o webssh ./cmd/webssh

WEBSSH_ADMIN_PASSWORD='replace-with-a-strong-password' ./webssh
```

Open `http://<server-lan-ip>:8080`. The default username is `admin`. On the first startup, `WEBSSH_ADMIN_PASSWORD` is required; passwords are never written to logs. Change the bootstrap password from Settings after signing in.

The default listener is `0.0.0.0:8080`, and persistent files are stored under `./data`.

## Docker

```bash
export WEBSSH_ADMIN_PASSWORD='replace-with-a-strong-password'
docker compose up -d
```

Tagged releases also publish multi-architecture images to:

```text
ghcr.io/zhangchaosd/webssh-gateway:<version>
```

## Production security

The default HTTP mode is intended only for initial evaluation on a trusted LAN. Production deployments should terminate TLS through Caddy, Nginx, or Traefik and set:

```bash
WEBSSH_PUBLIC_URL=https://ssh.example.lan
WEBSSH_ALLOWED_ORIGINS=https://ssh.example.lan
WEBSSH_COOKIE_SECURE=true
```

Keep the application behind a trusted LAN or VPN. The SQLite database and Master Key are both required to recover encrypted SSH credentials; back them up separately and restrict their permissions.

## Usage

1. Add an SSH password or private key under **Credentials**.
2. Add a target under **Hosts** and select the credential, group, and encoding.
3. On the first connection, verify the presented host-key fingerprint through a trusted independent channel, then pin it.
4. Future host-key changes are blocked instead of being accepted automatically.
5. Use the terminal toolbar to search scrollback, switch UTF-8/GB18030, or access mobile helper keys.

## Configuration

Configuration is supplied through environment variables. See [deploy/webssh.env.example](deploy/webssh.env.example) for the full example. Common values include:

| Variable | Default | Purpose |
|---|---|---|
| `WEBSSH_LISTEN` | `0.0.0.0:8080` | HTTP listen address |
| `WEBSSH_DATA_DIR` | `./data` | Persistent data directory |
| `WEBSSH_DATABASE` | `<data>/webssh.db` | SQLite database path |
| `WEBSSH_MASTER_KEY_FILE` | `<data>/master.key` | 32-byte Base64 Master Key file |
| `WEBSSH_ALLOWED_ORIGINS` | same host | Comma-separated additional origins |
| `WEBSSH_COOKIE_SECURE` | `false` | Require HTTPS for the session cookie |
| `WEBSSH_SESSION_IDLE_TIMEOUT` | `8h` | Web login session idle timeout |
| `WEBSSH_MAX_SESSIONS_PER_USER` | `10` | Per-user terminal limit |
| `WEBSSH_MAX_SESSIONS_TOTAL` | `50` | Process-wide terminal limit |

## Build and test

```bash
npm ci
npm run build
go test -race ./...
go vet ./...
go build -trimpath -o webssh ./cmd/webssh
```

Create a consistent SQLite backup after a WAL checkpoint:

```bash
./webssh backup --output /safe/path/webssh-backup.db
```

## Terminal protocol v1

- Client → server binary frames: raw terminal input bytes
- Server → client binary frames: raw SSH PTY output bytes
- Text/JSON frames: `resize`, `ping`/`pong`, `encoding`, `ready`, `exit`, and `error`
- Authentication, host authorization, and Origin validation happen before WebSocket upgrade
- Compression is disabled; message sizes, active sessions, and output buffering are bounded
- Exactly one goroutine writes WebSocket frames

See [API documentation](docs/API.md), [operations guide](docs/ADMIN.md), and [threat model](docs/THREAT_MODEL.md) for more details.

## License

[0BSD](LICENSE) — use, copy, modify, and distribute without attribution requirements.
