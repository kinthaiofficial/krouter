# CLAUDE.md — Project Guide for Claude Code

**Read this every time before making changes. Read DECISIONS.md too.**

---

## What is this project

**krouter** is a local LLM proxy daemon. It runs as a background service,
intercepts LLM requests from local AI agents (Claude Code, Cursor, OpenClaw),
and routes them to the cheapest suitable provider to save users tokens.

```
[Agent] → 127.0.0.1:8402 → [daemon] → [provider: anthropic/openai/deepseek/...]
            ↑                  ↑
            agent points        krouter decides
            here via env var    which provider to use
```

**Target users**: developers who use AI agents and want to save tokens.
They have GitHub access and basic technical literacy.

**License**: MIT. Forever free. No paid tier on the router itself.

---

## Architecture

Three ports, two binaries:

```
┌─────────────────────────────────────────────────────────────────────┐
│  krouter  (daemon + CLI + web UI)                                   │
│                                                                     │
│  ┌──────────────────────┐   ┌──────────────────────────────────┐   │
│  │  Proxy (8402)        │   │  Management (8403)               │   │
│  │  ALWAYS 127.0.0.1    │   │  127.0.0.1 default               │   │
│  │  No auth             │   │  Bearer token OR session cookie  │   │
│  │                      │   │                                  │   │
│  │  /v1/messages        │   │  /internal/status                │   │
│  │  /v1/chat/completions│   │  /internal/usage                 │   │
│  │  /health             │   │  /internal/preset                │   │
│  │                      │   │  /internal/settings GET+PATCH    │   │
│  │                      │   │  /internal/budget                │   │
│  │                      │   │  /internal/events  (SSE)         │   │
│  │                      │   │  /internal/auth/ticket           │   │
│  │                      │   │  /internal/auth/exchange         │   │
│  │                      │   │  /ui/*  (embedded React app)     │   │
│  └──────────────────────┘   └──────────────────────────────────┘   │
│                                                                     │
│  Background workers: pricing sync (24h), announcements (6h),       │
│                      auto-update check (24h), desktop notify        │
└─────────────────────────────────────────────────────────────────────┘

  krouter-installer  (one-shot wizard binary)
  ┌──────────────────────┐
  │  Install wizard (8404)│  opens browser → user steps through wizard
  │  Exits after setup   │  → writes service files, shell integration,
  └──────────────────────┘    agent configs → mints daemon session ticket
```

---

## Codebase map

```
cmd/
  krouter/              Main daemon + CLI binary (single binary, multi-subcommand)
    main.go             Entry point, version vars
    root.go             Cobra command tree
    serve.go            `krouter serve` — starts proxy + management ports
    cmd_install.go      `krouter install` — TTY installer
    cmd_uninstall.go    `krouter uninstall` — TTY uninstaller
    cmd_tray.go         `krouter tray` — starts system tray helper
  krouter-installer/    One-shot install wizard binary (exits after setup)
    main.go             Starts :8404 HTTP server, opens browser, waits for finalize

internal/
  proxy/               HTTP reverse proxy (8402, agent-facing)
  api/                 Management HTTP API + web UI (8403)
    server.go          Server struct, route registration, auth middleware
    auth.go            Ticket minting, session cookie exchange, authMiddleware
    ui_embed.go        go:embed of frontend/dist, /ui/* static serving + SPA fallback
  routing/             Decision engine: which provider to use
  providers/           Provider adapters (anthropic, openai, deepseek, ...)
  pricing/             LLM pricing table sync (LiteLLM JSON)
  storage/             SQLite persistence (~/.kinthai/data.db)
  notifications/       Notification center (CDN poll)
  upgrade/             Self-update via minio/selfupdate
  remote/              LAN remote daemon access (spec 10)
  config/              Settings (~/.kinthai/settings.json + fsnotify)
  logging/             Structured logging + rotation
  install/             Install orchestrator + GUI installer HTTP API
  uninstall/           Uninstall orchestrator
  notify/              Desktop notifications (beeep, CGO-free)
  webui/               go:embed packages for both frontends
    embed.go           Embeds frontend/dist (dashboard)
    installer/
      embed.go         Embeds frontend-install/dist (install wizard)

frontend/              Dashboard React app (served at /ui/ by daemon)
  src/
    api/client.ts      Typed API client
    pages/             Dashboard, Logs, Providers, Settings, Announcements, About
    components/        Layout, PresetSwitcher, QuotaBar, ...
  dist/                Built output (gitignored except .gitkeep; built by CI)

frontend-install/      Install wizard React app (served at / by krouter-installer)
  src/
    api/client.ts      Installer API client (reads token from URL ?token=)
    pages/             WelcomeStep, DetectStep, ServiceStep, ShellStep, DoneStep
  dist/                Built output (gitignored except .gitkeep; built by CI)

spec/                  Module specifications (READ before implementing)
packaging/             Platform packaging scripts (macos/, appimage/, windows.nsi)
scripts/               Release scripts (gen-manifest.go)
.github/workflows/     CI (ci.yml) + release pipeline (release.yml)
```

---

## Working rules

### 0. Before doing anything

1. Read `DECISIONS.md` — non-negotiable architecture decisions.
2. Read the relevant `spec/*.md` for the module you're touching.
3. If you're unsure how something should work, **ASK** before guessing.

### 1. Spec-driven implementation

The `spec/*.md` files are authoritative for HOW things should be built.

If spec is ambiguous or incomplete:
- Note the ambiguity as a TODO comment in the code
- Surface the question in your PR description
- DO NOT silently make assumptions

### 2. Module independence

Each `internal/<pkg>/` must be testable in isolation. Avoid:
- Cross-module direct DB calls (use the module's exported methods)
- Circular imports
- Tight coupling between provider adapter and pricing logic

### 3. Code style

- `gofmt -s -w .` and `goimports -w .` before commit
- `golangci-lint run` must pass
- Error wrapping: `fmt.Errorf("failed to X: %w", err)`
- Use `context.Context` for ALL operations that may block or be canceled
- Comments only where the WHY is non-obvious

### 4. Testing

- Each `internal/<pkg>/` MUST have `*_test.go` covering its public API
- Integration tests in `tests/integration/` use real HTTP servers (httptest)
- Run `make test` before any PR

### 5. NEVER do these

- ❌ Store API keys in SQLite / disk / memory beyond the request lifetime
- ❌ Send any user-identifiable data to any kinthai.ai endpoint
- ❌ Bind proxy port (8402) to anything other than 127.0.0.1
- ❌ Add login / sync / account features
- ❌ Add telemetry that uploads anything from daemon to our servers
- ❌ Require `sudo`/admin to install
- ❌ Convert between protocols (Anthropic ↔ OpenAI in the router)
- ❌ Modify RC files from daemon runtime (install wizard only, with marker comments)
- ❌ Use blocking I/O in hot path (proxy.HandleAnthropicMessages must stream)

### 6. Commit messages

```
<module>: <imperative subject line, ≤72 chars>

<optional body explaining WHY, not WHAT>

Refs: spec/<filename>.md
```

### 7. PR scope

One PR = one module = one spec section. Keep PRs under 500 lines net change.

---

## Authentication model

The management port (8403) supports two auth paths:

1. **Bearer token** — `Authorization: Bearer <token>` from `~/.kinthai/internal-token`.
   Used by CLI tools and tray helper.

2. **Session cookie** — `krouter_session=<sid>; HttpOnly; SameSite=Strict`.
   Used by the React web UI. Obtained via ticket exchange:
   ```
   POST /internal/auth/ticket  (Bearer required) → { ticket: "..." }  [30s TTL]
   GET  /internal/auth/exchange?ticket=...&redirect=/ui/  → 302 + Set-Cookie
   ```

The ticket exchange is `sync.Map.LoadAndDelete` — atomic, replay-safe.

`/health` and `/internal/auth/exchange` bypass auth middleware (auth endpoints themselves).

---

## Frontend builds

Both frontends must be built before `go build`:

```bash
cd frontend && npm ci && npm run build        # → internal/webui/dist/
cd frontend-install && npm ci && npm run build  # → internal/webui/installer/dist/
```

CI builds frontends as separate jobs and passes `dist/` as artifacts to the Go build job.
The `dist/` directories are gitignored except for `.gitkeep` placeholder files.

---

## Glossary

- **daemon**: the `krouter serve` process, background service
- **web UI**: the React app embedded in the daemon, served at `/ui/`
- **install wizard**: the React app served by `krouter-installer` at `:8404`
- **agent**: user's AI tool (Claude Code, Cursor, OpenClaw)
- **provider**: an LLM API endpoint (Anthropic, OpenAI, DeepSeek, etc.)
- **preset**: routing strategy (Saver / Balanced / Quality)
- **pairing token**: `KR-XXXX-XXXX-XXXX-XX`, for LAN remote daemon (spec 10)
- **proxy port**: 8402, agent-facing, always 127.0.0.1
- **management port**: 8403, web UI + API, switchable to 0.0.0.0 for LAN remote
- **installer port**: 8404, install wizard only, active while krouter-installer runs

---

## Key files quick reference

| File | Purpose |
|------|---------|
| `CLAUDE.md` | This file — read every time |
| `DECISIONS.md` | Non-negotiable architecture decisions |
| `spec/*.md` | Per-module specifications |
| `Makefile` | Build / test / lint commands |
| `go.mod` | Dependencies |
| `.github/workflows/release.yml` | Release pipeline (builds all platforms + packages) |
