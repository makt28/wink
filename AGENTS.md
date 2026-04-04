# AGENTS.md

This file provides guidance to Codex (Codex.ai/code) when working with code in this repository.

## Build & Development Commands

```bash
make build        # tailwind + go fmt + go vet + go build -o wink ./cmd/server
make tailwind     # npx tailwindcss -i ... -o web/static/tailwind.css --minify
make dev          # go run ./cmd/server
make fmt          # go fmt ./...
make vet          # go vet ./...
make test         # go test ./...
make clean        # rm -f wink
make docker       # docker build -t wink -f deploy/Dockerfile .
```

Tailwind CSS is compiled locally (requires Node.js + `npm install`). The output `web/static/tailwind.css` is embedded into the binary via `go:embed`.

Run a single test: `go test -v -run TestFoo ./internal/config/`

ICMP probing uses the system `ping` command — no special privileges needed.

## Architecture

Wink is a single-binary self-hosted uptime monitor. Go backend with vanilla JavaScript frontend, all embedded via `go:embed`.

**Tech:** Go 1.24 + chi router + `html/template` + vanilla JS + Tailwind CSS (compiled locally, embedded). Dependencies: `go-chi/chi`, `golang.org/x/crypto` (bcrypt). Build-time: Node.js + tailwindcss. ICMP uses system `ping` command.

### Module Map

| Module | Path | Role |
|---|---|---|
| Config Manager | `internal/config/` | Load/save/validate `config.json`, broadcast changes via channel, `sync.RWMutex` |
| Scheduler | `internal/monitor/scheduler.go` | 1 goroutine per monitor, diffs config changes, dynamic interval switching |
| Prober | `internal/monitor/prober.go` | `Prober` interface: HTTP, TCP, ICMP implementations |
| Analyzer | `internal/monitor/analyzer.go` | Flapping control, retry counting, UP/DOWN transitions, incident creation |
| Notification Router | `internal/notify/router.go` | Routes alerts to per-monitor `notifier_ids`, builds notifiers from config |
| History Manager | `internal/storage/history.go` | Ring buffer for latency, separate incident storage, atomic dump of both files |
| Web Layer | `internal/web/` | Chi routes, auth middleware, session store, rate limiter, page/API handlers |

### Data Flow

```
Scheduler → Monitor Goroutine → Prober (HTTP/TCP/ICMP) → ProbeResult
    → Analyzer (flapping control) → Status Change? → NotificationRouter → Telegram/Webhook
                                  → HistoryManager → history.json + incidents.json (atomic write)
```

### Key Design Patterns

**Atomic file writes:** All JSON persistence uses write-to-temp → `Sync()` → `os.Rename()`.

**Tiered persistence:** Status changes (DOWN/UP) trigger immediate dump. Latency data dumps on configurable interval (default 300s). Graceful shutdown dumps everything.

**Flapping control:** `failCount` tracks consecutive failures. Alert fires only when `failCount >= MaxRetries`. After DOWN, optional `ReminderInterval` re-sends every N failures. `RetryInterval` switches to faster probing when failing.

**Per-monitor notifier targeting:** Notification routing uses `monitor.notifier_ids` (list of notifier IDs), not contact groups. Groups are visual organization only. Empty `notifier_ids` = no notifications.

**Monitor enabled field:** `Enabled *bool` — nil means true (default). Use `monitor.IsEnabled()` helper.

### Storage Files

| File | Content |
|---|---|
| `config.json` | All configuration (system, auth, groups, monitors) |
| `history.json` | Latency ring buffer + uptime percentages per monitor (no incidents) |
| `incidents.json` | Incident records per monitor, 30-day retention with auto-cleanup |

Incidents were migrated from `history.json` to separate `incidents.json`. The `HistoryManager` handles both files and auto-migrates on first load.

### Routes

**Public:** `GET /login`, `POST /login`, `GET /healthz`, `GET /lang?l=`, `GET /theme?t=`, `/static/*`

**Protected (auth required):** `GET /`, `GET /monitors/new`, `POST /monitors`, `GET /monitors/{id}/edit`, `POST /monitors/{id}`, `POST /monitors/delete`, `GET /api/monitors`, `GET /api/monitors/{id}`, `POST /api/monitors/{id}/toggle`, `GET /settings`, `POST /settings/system`, `POST /settings/auth`, `POST /settings/groups`, `POST /settings/groups/delete`, `POST /settings/groups/notifiers`, `POST /settings/groups/notifiers/delete`, `POST /settings/notifiers`, `POST /settings/notifiers/update`, `POST /settings/notifiers/delete`, `POST /api/notifiers/{id}/test`, `POST /api/telegram/get-updates`, `POST /logout`

## Frontend

Dashboard is vanilla JavaScript (`web/static/app.js`) polling `/api/monitors` and `/api/monitors/{id}` every 10 seconds. Templates render minimal HTML; JS builds the monitor list and detail views dynamically. Polling pauses when the page is hidden (`document.hidden`). Selected monitor persists in `sessionStorage`.

### i18n

- Translation files: `web/i18n/en.json`, `web/i18n/zh.json`
- Templates: `{{t .Lang "key"}}` via FuncMap
- JS translations: registered in `jsI18nKeys` slice in `internal/web/router.go`, injected as `window.I18N` object, accessed via `t("key")` in app.js
- Language cookie: `wink_lang`, switched via `GET /lang?l=en|zh`
- All user-facing text must use i18n keys, never hardcoded strings

### Template Rendering

Templates use a two-tier system: `layout.html` provides the wrapper (nav, theme, i18n), page templates define `{{define "content"}}` blocks. `login.html` is standalone (no layout). Embedded via `web/embed.go` (`webassets` package).

### Theme

Cookie-based (`wink_theme`): light/dark, toggled via `GET /theme?t=light|dark`. CSS uses Tailwind `dark:` variant classes. Inline script in layout prevents FOUC.

## Key Config Structs

**NotifierConfig** has `ID` (auto-generated hex), `Type` (telegram/webhook), `Remark` (optional label shown in alerts), plus type-specific fields.

**Monitor** has `NotifierIDs []string` for per-monitor targeting and `Enabled *bool` for pause/resume.

**SystemConfig** has `Timezone string` (IANA name) for notification timestamps.

## Conventions

- Never commit `config.json` — use `config.json.example`
- All exported methods on `ConfigManager`, `HistoryManager`, `SessionStore` must be goroutine-safe (they own their mutexes)
- Error handling: return errors up, log at top level with `slog`
- `Prober` and `Notifier` are interface-based for extensibility
- Run `go fmt ./...` before commits
