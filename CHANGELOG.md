# Changelog

All notable changes to krouter will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [2.0.22] - 2026-05-18

### Fixed
- **"KRouter took too long to start" — 根本原因** — 管理 API（port 8403）
  从未注册 `/health` 路由；installer 的 `daemon-ready` 轮询一直得到 404
  而非 200，永远返回 `{ready: false}`，导致 60 s 超时。现在 `/health` 已
  注册且无需认证，1 个测试验证其行为。之前对 `waitPortFree`、`bootout`、
  `stopRunningDaemon` 的修复解决的是"两个进程"和"拖拽安装"问题，
  但超时 bug 的真正原因一直是这个缺失的端点。

---

## [2.0.21] - 2026-05-18

### Fixed
- **macOS: "krouter is still running" / 两个 krouter 进程 / 无法升级** —
  `krouter-installer` 现在启动时的第一个动作就是调用 `launchctl bootout`，
  将正在运行的旧 daemon 从 launchd 监管中完全移除。此前：旧 daemon 注册了
  `KeepAlive=true`，用户手动杀进程后 launchd 立即重启，导致永远无法替换；
  macOS Finder 看到同名进程报错 "krouter is still running"，用户无从下手。
  现在用户只需打开新版安装包，installer 会自动停掉旧进程再走安装流程，
  无需任何手动操作。Linux/Windows 同理（systemctl stop / schtasks /End）。

---

## [2.0.20] - 2026-05-18

### Fixed
- **macOS/Linux/Windows: "KRouter took too long to start" / two krouter processes** —
  the proxy port-conflict guard in `serve.go` previously exited immediately (return nil)
  when port 8402 was already in use. During reinstall the old binary is still shutting
  down when launchd/systemd starts the new one, so the new binary would exit, launchd
  would wait its default 10 s ThrottleInterval before retrying, and the installer's
  60 s poll window would time out. The guard now polls every 100 ms (up to 10 s) for
  the port to be freed; the new binary picks it up as soon as the old one releases it
  with no ThrottleInterval delay. Falls back to silent exit only if the port is still
  busy after 10 s (a permanent instance is genuinely running).
- 4 unit tests covering `waitPortFree`: immediate return when free, waits until
  released, times out correctly, distinguishes bound vs free address

---

## [2.0.19] - 2026-05-18

### Added
- **`krouter start` / `krouter stop` CLI commands**: proper daemon lifecycle management
  - macOS: `launchctl bootstrap gui/<uid> <plist>` / `launchctl bootout gui/<uid> <plist>`
  - Linux: `systemctl --user start krouter` / `systemctl --user stop krouter`
  - Windows: `schtasks /Run /TN krouter-daemon` / `schtasks /End /TN krouter-daemon`

### Changed
- **macOS: `LoadLaunchAgent` now uses `launchctl bootstrap/bootout`** instead of the
  deprecated `load/unload` + `pgrep` polling approach. `launchctl bootout` is
  synchronous and waits for the process to fully exit before returning, eliminating
  the port-conflict race without any process-name polling.

### Removed
- `processExists` (pgrep-based process checker) — no longer needed now that
  `launchctl bootout` provides synchronous process exit semantics

---

## [2.0.18] - 2026-05-18

### Fixed
- **macOS: two krouter processes / "KRouter took too long to start"** —
  `launchctl unload` sends SIGTERM asynchronously and returns before the
  process exits; the immediately-following `load` would start the new binary
  while the old one still held the ports, causing the new binary to silently
  exit (port-conflict guard in `serve.go`). launchd then waited its default
  10 s before retrying, exceeding the installer's 60 s poll window.
  `LoadLaunchAgent` now calls `WaitForProcessExit` after unload, polling
  `pgrep -x krouter` every 100 ms (up to 5 s) before issuing `load`, so
  the new binary always starts against free ports.

### Added
- `config.WaitForProcessExit(name, timeout, interval, checkFn)` — injectable
  process-exit poller extracted from `LoadLaunchAgent` for unit testing
- 6 unit tests for `WaitForProcessExit`: immediate return when already gone,
  polls until checker returns false, times out correctly, passes correct name
  to checker, zero-timeout returns immediately, exits on first false response

---

## [2.0.17] - 2026-05-18

### Added
- **Installer tests — DoneStep (13 cases)**: covers initial render, spinner on click,
  navigation on first poll, navigation after later poll, timeout error (correct
  `/krouter/` fallback URL), network-failure timeout, retry after timeout, error
  cleared on retry, finalize-410 swallowed, finalize-500 swallowed, missing
  `redirect_url` triggers fallback error
- **Installer tests — ShellStep (13 cases)**: covers initial render, Skip without
  API call, Applying… spinner, success banner + Open Dashboard button, API error
  message + retry, finalize called before first poll, navigation to redirect_url,
  timeout error with `/krouter/` URL, connection-refused timeout, button reappears
  for retry, finalize-410 swallowed during Open Dashboard flow

### Changed
- `DoneStep` and `ShellStep` accept `maxAttempts` and `pollIntervalMs` props
  (default 40 / 1500 ms) to enable deterministic testing without fake timers

---

## [2.0.16] - 2026-05-18

### Changed
- **Dashboard URL**: management UI now served at `/krouter/` instead of `/ui/` —
  bookmarks and shell output now show `http://127.0.0.1:8403/krouter/`
- **Dashboard branding**: sidebar, active-nav highlight, preset buttons, quota bars,
  and action buttons now use the KRouter green brand palette (`#25d366`) to match
  the installer wizard; sidebar shows the KRouter logo and version tag
- **Routing Preset buttons**: clicking a preset now gives immediate visual feedback
  (optimistic update) instead of waiting for the server round-trip to confirm

### Fixed
- **Providers page**: raw `fetch` now throws on non-2xx responses and surfaces an
  error message instead of silently rendering an empty list
- **Providers page**: MiniMax added to the known-providers list so its setup hint
  (`Set MINIMAX_API_KEY to enable`) appears when the key is not configured

---

## [2.0.15] - 2026-05-18

### Fixed
- **macOS: "Open KRouter Dashboard" showed connection error on first install** —
  two root causes fixed:
  1. `launchctl load -w` is a no-op when the service is already loaded (reinstall
     case), leaving the old process running; `LoadLaunchAgent` now unloads first so
     the daemon is always restarted with the updated binary.
  2. Even with the unload fix, timing cannot be fully guaranteed; the installer now
     shows a "Starting KRouter daemon…" spinner and polls `/api/install/daemon-ready`
     every 1.5 s (up to 60 s) before navigating to the dashboard, so the browser
     only opens the URL once the daemon is actually accepting connections.
- **macOS: skipping shell integration left `MarkInstalled` uncalled** — `DoneStep`
  now calls `finalize` (idempotent) before polling, ensuring the
  `~/.kinthai/installed` marker is always written regardless of which path through
  the wizard the user takes.

---

## [2.0.14] - 2026-05-17

### Fixed
- **macOS: install wizard opened a second browser tab at port 8405** — `krouter-installer`
  passed no `SrcBinary` to the orchestrator, so `CopyBinary()` fell back to
  `os.Executable()` (the installer itself) and copied it to `~/.local/bin/krouter`;
  the LaunchAgent then started `krouter-installer` instead of `krouter`, which spawned
  a fresh wizard on port 8405 and opened a new browser tab showing the installer's
  first page. Fixed by detecting the co-located `krouter` daemon binary (e.g. inside
  the `.app` bundle's `Contents/MacOS/`) and using it as `SrcBinary`.
- **macOS: "Open KRouter Dashboard" button navigated before daemon was ready** —
  `handleFinalize` now polls `:8403/health` for up to 10 s before minting the session
  ticket, ensuring the redirect URL carries a valid ticket instead of falling back to an
  unauthenticated `/ui/` URL; the button shows "Opening dashboard…" while waiting.

---

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
