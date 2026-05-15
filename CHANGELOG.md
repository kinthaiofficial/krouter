# Changelog

All notable changes to krouter will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [2.0.13] - 2026-05-15

### Fixed
- **Linux: shell integration written to wrong file** — `DetectShellRC()` mapped bash
  to `~/.bash_profile` on all platforms; bash on Linux now correctly targets
  `~/.bashrc` (macOS keeps `~/.bash_profile`, which is correct for macOS login shells)
- **Daemon token clobbering on port conflict** — when `install --yes` triggered
  multiple rapid daemon starts (e.g. idempotent re-runs), each short-lived instance
  would overwrite `~/.kinthai/internal-token` before failing to bind, leaving the
  real daemon holding a stale token; `serve` now exits silently before writing the
  token if the proxy port is already bound

---

## [2.0.12] - 2026-05-15

### Fixed
- **SQLite driver replaced with pure-Go implementation** — switched from
  `mattn/go-sqlite3` (requires CGO) to `modernc.org/sqlite` (pure Go), enabling
  `CGO_ENABLED=0` cross-compilation; removes the CGO toolchain requirement from
  all build environments including Windows cross-compilation

---

## [2.0.11] - 2026-05-15

### Fixed
- **MiniMax base URL corrected** — domestic (Chinese mainland) API keys only work
  on `api.minimax.chat`; the adapter was incorrectly using `api.minimax.io`

---

## [2.0.10] - 2026-05-15

### Fixed
- **Windows: daemon not starting after installation** — three root causes resolved:
  1. `service_other.go` build tag (`!linux && !darwin`) inadvertently caught Windows,
     making `RegisterService()` a silent no-op; replaced with a proper
     `service_windows.go` that calls `RegisterTask` + `StartTask`
  2. NSIS script registered the Task Scheduler task but never ran it, so the
     daemon only started on the *next login*; added `schtasks /Run /TN "krouter-daemon"`
     immediately after `task-install`
  3. `orchestrator.RegisterService()` hardcoded `~/.local/bin/krouter` (a Linux
     path); replaced with `platformDaemonPath()` which returns
     `%LOCALAPPDATA%\kinthai\krouter.exe` on Windows

### Added
- `config.StartTask()` — runs the Task Scheduler task immediately via
  `schtasks /Run` (Windows only; stub on other platforms)
- `config.TaskName()` — exports the task name constant (`"krouter-daemon"`)

---

## [2.0.9] - 2026-05-15

### Fixed
- **Transparent proxy correctness**: removed accidental API key injection from the
  Anthropic-protocol adapter. krouter now forwards the client's `x-api-key` header
  unchanged to all Anthropic-compatible upstreams (including MiniMax). Previously,
  MiniMax requests incorrectly attempted to inject a server-side `MINIMAX_API_KEY`.

### Added
- Test coverage for ticket and session expiry (`TestExchangeTicket_ExpiredFails`,
  `TestSessionCookie_ExpiredFails`)
- `TestOrchestrator_ShellIntegration_Fish` — verifies Fish shell `config.fish` path
  is created with the krouter marker block
- `TestListen_PortConflict_TriesNext` — verifies install server binds the next port
  when the requested port is already occupied
- `TestWriteLaunchAgentPlist_ReturnsError_OnNonMacOS` — verifies the macOS
  LaunchAgent stub returns an error on Linux/Windows

---

## [2.0.8] - 2026-05-14

### Added
- MiniMax provider adapter (`internal/providers/minimax`): Anthropic-messages
  protocol at `https://api.minimax.io/anthropic`, enabled via `MINIMAX_API_KEY`
- Routing engine now prefers the provider that explicitly lists the requested model
  (`pickProviderForModel`), preventing MiniMax-model requests from being mis-routed
  to the Anthropic upstream

---

## [2.0.7] - 2026-05-13

### Fixed
- PATCH `/internal/settings` returned 503 because `apiSrv.SetSettings()` was never
  called in `serve.go`

---

## [2.0.6] - 2026-05-13

### Fixed
- SPA routes (e.g. `/ui/logs`) returned HTTP 301 due to `http.FileServer`'s
  `/index.html` → `./` canonicalization; fixed by reading `index.html` directly
- Linux systemd user service: `krouter install --yes` now runs `loginctl enable-linger`
  and sets `XDG_RUNTIME_DIR=/run/user/<uid>` before calling `systemctl --user`, so
  installation works on SSH-only servers without an active login session

---

## [2.0.0] - 2026-05-13

**BREAKING**: Replaced Wails v2 desktop GUI with embedded React web UI served by
the daemon. This changes the install flow, binary distribution, and GUI entry point.

### Changed
- **GUI architecture**: daemon now embeds React web UI at `http://127.0.0.1:8403/ui/`
  instead of shipping a separate Wails desktop binary
- **Install flow**: replaced Wails app with `krouter-installer`, a standalone binary
  that serves a browser-based wizard at `:8404` (no Electron, no native window)
- **Two-binary distribution**: `krouter` (daemon + CLI) + `krouter-installer`
  (one-shot wizard, exits after setup completes)
- **Authentication**: management API now supports session cookies in addition to
  Bearer tokens; ticket-exchange flow allows tray/CLI to open the web UI without
  re-entering credentials

### Added
- `krouter install` — TTY installer with `--yes` / `--dry-run` / `--skip-agents` flags
- `krouter uninstall` — uninstaller with `--keep-data` flag
- Web UI pages: Dashboard, Logs, Providers, Settings, Announcements, About
- SSE event stream at `GET /internal/events` for real-time UI updates
- Desktop notifications via `gen2brain/beeep` (CGO-free; quota warnings, announcements,
  upgrade available)
- Session cookie auth (`POST /internal/auth/ticket` → `GET /internal/auth/exchange`)
- `GET /internal/settings` and `PATCH /internal/settings` endpoints
- `GET /internal/budget` endpoint (today's savings breakdown)
- macOS `.dmg` release artifact (krouter.app bundle, `LSUIElement=true`)
- Linux `.AppImage` release artifact
- Windows `krouter-setup.exe` NSIS installer (launches wizard on completion)
- Signed `manifest.json` + `manifest.json.sig` on every release

### Removed
- `cmd/krouter-gui/` — Wails v2 desktop application and all Wails dependencies

---

## [1.0.3] - 2026-05-13

Patch release; CI pipeline fixes only. No functional changes from v1.0.0.

---

## [1.0.0] - 2026-05-13

Initial release (Wails-based architecture; superseded by v2.0.0).

### Added
- HTTP proxy on 127.0.0.1:8402 (Anthropic + OpenAI protocols)
- Routing engine with Balanced + Saver presets
- Providers: Anthropic, OpenAI, DeepSeek, Groq, Moonshot, GLM, Qwen
- LaunchAgent / systemd --user / Windows Task Scheduler integration
- SQLite-based per-request logging
- Notification center with 6h CDN poll
- Self-update with ECDSA manifest verification
- LAN remote access with HTTPS and pairing tokens
- CLI: status, logs, budget, config, test
