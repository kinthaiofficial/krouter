# DECISIONS.md — Non-Negotiable Architecture Decisions

These decisions are **fixed**. Don't reopen them in PRs. If you genuinely believe
one needs to change, raise it as a separate discussion with the maintainer first —
do not silently violate.

---

## Product

### D-001: Forever free, MIT licensed

The router is open source under MIT, free forever. There is no "paid tier".
Monetization (if any) happens elsewhere — never on this repo.

### D-002: Zero data upload

The daemon **never** sends user-identifiable data to any kinthai.ai server.
The only outbound calls are:
1. Proxy traffic to user-configured LLM providers (their data, their choice)
2. Poll `announcements.kinthai.ai/feed.json` (anonymous GET, only User-Agent)
3. Auto-update check to GitHub Releases (anonymous GET)

Never add `/heartbeat`, `/install-event`, `/analytics`. Ever.

### D-003: API keys never persisted

API keys come from the agent's environment, pass through in request headers,
and forward unchanged to the upstream provider. **They never touch SQLite.
They never touch disk. They never live longer than the request.**

### D-004: Target users have GitHub access

We do NOT optimize for users who cannot access GitHub/Cloudflare/Western
infrastructure. This product is not for users behind aggressive content filtering.

---

## Architecture

### D-010: Two-binary, three-port model

The product is **two independent binaries**:
- `krouter` — daemon (headless, managed by OS service manager) + CLI + embedded web UI
- `krouter-installer` — one-shot install wizard (exits after setup completes)

Three ports, strict roles:

```
8402  Proxy         ALWAYS 127.0.0.1, no auth, agent-facing
8403  Management    127.0.0.1 default, optional 0.0.0.0, Bearer/cookie auth
8404  Installer     127.0.0.1 only, active only during krouter-installer run
```

Closing the browser tab does NOT stop routing — the daemon keeps running.
The installer process exits after the user completes the wizard.

### D-011: Proxy port is a hard security boundary

Proxy port (8402) is **ALWAYS** 127.0.0.1. API keys are in those request headers.
Never bind 0.0.0.0 to the proxy port. Ever.

Management port can switch to 0.0.0.0 only when user explicitly enables remote
access (spec 10). Even then: full Bearer auth + self-signed HTTPS.

### D-012: Zero-elevation install

No `sudo`. No admin password. No UAC prompt. All install in user directories:

```
macOS:   LaunchAgent (~/Library/LaunchAgents/), NEVER LaunchDaemon
Linux:   systemd --user (~/.config/systemd/user/), NEVER system-wide unit
Windows: Task Scheduler user task (HKCU), NEVER Windows Service
```

### D-013: Same-protocol routing only

The router NEVER converts between protocols. An Anthropic request routes to
an Anthropic-protocol provider. An OpenAI request routes to an OpenAI-protocol
provider.

### D-014: SQLite for persistence, file-based for config

- SQLite (`~/.kinthai/data.db`) — request logs, quota state, provider status,
  pricing cache, announcements, paired_devices, web_sessions, auth_tickets
- JSON file (`~/.kinthai/settings.json`) — user settings (watched via fsnotify)
- No remote DB. No cloud sync.

### D-015: Notification center is read-only from CDN

`announcements.kinthai.ai/feed.json` is the only source. ETag mandatory.
99% of polls return 304. Failure tolerance > engineering complexity.

---

## User Experience

### D-020: Users make decisions, never operate

"User must edit a config file" is a product failure.

| Wrong (user operates)              | Right (wizard / UI operates)                 |
|------------------------------------|----------------------------------------------|
| "Edit ~/.openclaw.json and add…"   | One-click "Enable Routing" in install wizard |
| "Add this to your ~/.zshrc…"       | Install wizard writes marker-wrapped block   |
| "Run `launchctl load …`"           | Install wizard handles LaunchAgent setup     |

### D-021: GUI install wizard is the primary distribution path

MVP distributes via:
- macOS: `.dmg` (krouter.app bundle, `LSUIElement=true`)
- Windows: `krouter-setup.exe` (NSIS, user-level install)
- Linux: `.AppImage` (chmod +x; double-click)

brew/winget/Flathub are M4+ additions, not primary path.

### D-022: Web UI never accepts API keys

API keys come from the agent's existing config. The web UI shows status but
cannot input keys. This prevents accidental key leakage to disk.

---

## Implementation

### D-030: Go backend, React web UI (embedded in daemon)

- Go 1.22+
- React 18 + TypeScript + Vite + Tailwind CSS for web UI
- Web UI embedded in daemon binary via `go:embed` (no separate server process)
- SQLite via `mattn/go-sqlite3` (CGO)
- Cobra for CLI

**No Electron. No Wails. No native window.** The "GUI" is a browser tab.

### D-031: ECDSA P-256 for update signing

Releases signed with ECDSA P-256. Public key embedded in binary.
Auto-update verifies signature before applying.

### D-032: Self-signed certs for LAN remote (spec 10)

LAN remote access uses self-signed HTTPS + SSH-style trust-on-first-use.
Not Let's Encrypt. Not mTLS.

### D-033: 18-char Base32 Crockford for pairing tokens

`KR-XXXX-XXXX-XXXX-XX` format. Base32 Crockford (no 0/O/1/I/U).

### D-040: Web UI auth uses ticket exchange, not direct token exposure

The browser never sees the internal Bearer token. Flow:
1. CLI/tray calls `POST /internal/auth/ticket` (Bearer) → short-lived ticket (30s)
2. Browser hits `GET /internal/auth/exchange?ticket=...` → session cookie (8h)
3. Browser uses cookie for all subsequent requests

Ticket exchange is `sync.Map.LoadAndDelete` — atomic, replay-proof.

### D-041: Desktop notifications are CGO-free (beeep)

Desktop notifications use `gen2brain/beeep`:
- macOS: `osascript`
- Linux: `notify-send`
- Windows: toast via `go-toast`

No systray process required for notifications. 5-minute dedup window per event type.

### D-042: Install wizard is a separate binary that exits after setup

`krouter-installer` is NOT a long-running process. It:
1. Generates a one-time install token
2. Starts HTTP server on :8404
3. Opens the user's default browser
4. Serves the React install wizard
5. On `POST /api/install/finalize`, mints a daemon session ticket
6. Returns a redirect URL to the daemon web UI
7. Exits

### D-043: No console window for Windows installer

`krouter-installer` on Windows is built with `-H windowsgui` (linker flag).
No `cmd.exe` window appears when the user double-clicks the installer.

---

## Things explicitly NOT to do

Each is a rejected design path. Listed so future contributors don't reintroduce them.

- ❌ kinthai.ai login / OAuth in router
- ❌ Cross-device settings sync via cloud
- ❌ Telemetry / analytics upload from daemon
- ❌ Cross-protocol conversion (Anthropic ↔ OpenAI in router)
- ❌ Storing API keys in any form beyond the request lifetime
- ❌ LaunchDaemon / Windows Service / system-wide systemd (requires elevation)
- ❌ Electron, Wails, or any native window framework for the dashboard
- ❌ "Free version" + "Pro version" (it's all free)
- ❌ Binding proxy port (8402) to 0.0.0.0

---

## How to propose changing a decision

1. Open an issue titled `DECISION-CHANGE: D-XXX <topic>`
2. Describe what changed in the world that invalidates the decision
3. Cost-benefit analysis
4. Maintainer approval before any code change

Do not silently violate a decision and "see if anyone notices in review."
