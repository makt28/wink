# Wink

[中文文档](README_CN.md)

A minimal, high-performance, single-binary self-hosted monitoring tool.

Lightweight alternative to Uptime Kuma — zero dependencies, file-based storage, single executable.

## Features

- **Single binary** — Go backend + embedded frontend, no runtime dependencies
- **File-based storage** — JSON config and history, no database required
- **HTTP / TCP / ICMP** monitoring with configurable intervals
- **Flapping control** — debounce alerts with retry thresholds, no false alarms
- **Reminder alerts** — repeat notifications every N failures after DOWN
- **Dynamic retry interval** — faster probing when a monitor is failing
- **Telegram & Webhook** notifications with extensible notifier interface
- **Notifier remark** — label each notifier for easy identification in alert messages
- **Inline notifier management** — edit, test, and delete notifiers directly from settings
- **Telegram Chat ID helper** — fetch available chats from Bot API with one click
- **Per-monitor notifier targeting** — send alerts to specific notifiers only
- **Monitor pause/resume** — temporarily disable monitors without deleting them
- **Grouped monitor list** — monitors organized by group with collapsible sections
- **Uptime tracking** — 24h / 7d / 30d sliding window calculations
- **Heartbeat bars** — visual history of recent probe results per monitor
- **Incident log** — separate 30-day incident storage (`incidents.json`) with automatic cleanup
- **Timezone support** — auto-detects system timezone on first launch, configurable via UI
- **SSO** — reverse proxy Single Sign-On via `Remote-User` header
- **Friendly error handling** — inline toast notifications for form validation errors
- **Atomic writes** — crash-safe file persistence (write-sync-rename)
- **Login rate limiting** — per-IP lockout after failed attempts
- **Session TTL** — auto-expiring sessions with background cleanup
- **Hot reload** — add/edit/remove monitors without restart
- **Web settings** — configure system, auth, groups, and notifiers from the UI
- **i18n** — Chinese / English bilingual interface with one-click switching
- **Dark mode** — light / dark theme toggle
- **Health endpoint** — `GET /healthz` for external monitoring

## Quick Start

### Linux one-click install (recommended)

Requires: Linux (amd64/arm64), systemd, curl.

```bash
curl -fsSL https://raw.githubusercontent.com/makt28/wink/main/install.sh | sudo bash
```

After installation, use the `wink` command to manage the service:

```bash
sudo wink start       # Start service
sudo wink stop        # Stop service
sudo wink restart     # Restart service
sudo wink status      # Show status
sudo wink logs        # Follow logs (Ctrl+C to exit)
sudo wink update      # Update to latest release
sudo wink uninstall   # Remove Wink (preserves data)
sudo wink reinstall   # Re-download and restart
```

Data files are stored in `/opt/wink/`.

### Download binary

Download the latest release for your platform from [Releases](https://github.com/makt28/wink/releases):

| Platform | File |
|---|---|
| Linux x86_64 | `wink-linux-amd64` |
| Linux ARM64 | `wink-linux-arm64` |
| macOS Apple Silicon | `wink-darwin-arm64` |
| Windows x86_64 | `wink-windows-amd64.exe` |

```bash
chmod +x wink-linux-amd64
./wink-linux-amd64
```

> **Termux (Android):** Use `wink-linux-arm64` — it runs natively on Termux's Linux kernel.

### Build from source

Requires Go 1.24+ and Node.js (for Tailwind CSS compilation).

```bash
git clone https://github.com/makt28/wink.git
cd wink
npm install
make build
./wink
```

### Cross-compile

Build for all supported platforms at once:

```bash
make cross
```

Outputs to `dist/`:

```
dist/wink-linux-amd64
dist/wink-linux-arm64
dist/wink-darwin-arm64
dist/wink-windows-amd64.exe
```

### Login

Open `http://localhost:8080` in your browser.

Default credentials: **admin** / **123456**

Change the password in **Settings > Auth** after first login.

## Configuration

See `config.json.example` for the full schema. Key sections:

| Section | Description |
|---|---|
| `system` | Bind address, check interval, history limits, log level, timezone (auto-detected) |
| `auth` | Username, bcrypt password hash, login rate limiting, SSO toggle |
| `contact_groups` | Visual grouping for monitors |
| `notifiers` | Notification channels (Telegram, Webhook) with remark labels |
| `monitors` | List of targets to monitor (HTTP, TCP, Ping) |

### Monitor fields

| Field | Description | Default |
|---|---|---|
| `interval` | Check interval in seconds | System default |
| `timeout` | Probe timeout in seconds | 5 |
| `max_retries` | Failures before marking DOWN | 3 |
| `retry_interval` | Faster interval when failing (0 = normal) | 0 |
| `reminder_interval` | Re-alert every N failures after DOWN (0 = off) | 0 |
| `ignore_tls` | Skip TLS certificate validation (HTTP only) | false |
| `enabled` | Enable/disable the monitor (null = true) | true |
| `notifier_ids` | Send alerts to specific notifiers only (empty = no notifications) | [] |

### Monitor types

| Type | Target format | Example |
|---|---|---|
| `http` | Full URL | `https://api.example.com/health` |
| `tcp` | `host:port` | `db.example.com:5432` |
| `ping` | Hostname or IP | `10.0.0.1` |

> **Note:** Ping uses the system `ping` command — no special privileges needed. Make sure `ping` is available in your `PATH`.

### Data files

| File | Description |
|---|---|
| `config.json` | All configuration (system, auth, groups, monitors) |
| `history.json` | Latency history and uptime data per monitor |
| `incidents.json` | Incident records, auto-cleaned after 30 days |

## Development

```bash
# Run directly
make dev

# Format code
make fmt

# Static analysis
make vet

# Run tests
make test

# Full build (tailwind + fmt + vet + build)
make build

# Rebuild Tailwind CSS only
make tailwind

# Cross-compile all platforms
make cross
```

## API

### Health check

```
GET /healthz
```

Returns (no auth required):

```json
{
  "status": "ok",
  "version": "0.1.0",
  "uptime_seconds": 86400,
  "monitor_count": 5
}
```

## Architecture

```
Scheduler → 1 goroutine per monitor → Prober (HTTP/TCP/ICMP)
         → Analyzer (flapping control) → Notification Router → Telegram / Webhook
                                       → History Manager → history.json + incidents.json (atomic write)
```

- **Frontend:** Vanilla JavaScript + Tailwind CSS (compiled & embedded), all via `go:embed`
- **Backend:** Go + chi router, `html/template` rendering
- **Storage:** Atomic JSON file writes (write → sync → rename)

## License

GPL-3.0
