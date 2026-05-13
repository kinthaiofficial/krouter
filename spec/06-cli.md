# spec/06-cli.md — CLI Tools

**Module**: `cmd/krouter` (same binary as daemon)
**Subcommands**: serve, status, shell-init, config, budget, logs, test, version, remote, pair

---

## 1. Design philosophy

The same Go binary serves three roles:
1. **Daemon** (`krouter serve`) — long-running process
2. **CLI** (`krouter status` etc.) — short-lived, talks to daemon
3. **Web UI companion** (krouter-installer opens the browser UI after install)

Web UI users never need CLI. CLI is for:
- Power users / scripting
- CI/CD integration
- Debugging
- Headless servers (no GUI environment)

---

## 2. CLI architecture

Each CLI subcommand (except `serve` and `shell-init`):
1. Reads `~/.kinthai/internal-token` (0600 file written by daemon)
2. HTTP GET/POST to `http://127.0.0.1:8403/internal/...` with `Authorization: Bearer <token>`
3. Formats response for terminal output (TTY-aware: color if terminal, plain if pipe)
4. Exits with appropriate code

---

## 3. Subcommand specifications

### 3.1 `krouter serve`

Daemon mode. Detailed in spec/01-proxy-layer.md.

```
$ krouter serve --proxy-port=8402 --management-port=8403
  Starting krouter daemon...
  ✓ Loaded settings from ~/.kinthai/settings.json
  ✓ Database open at ~/.kinthai/data.db
  ✓ Proxy listening on 127.0.0.1:8402
  ✓ Management API listening on 127.0.0.1:8403
  Ready.
```

### 3.2 `krouter status`

```
$ krouter status

krouter daemon
  Status:        Running
  Version:       1.0.0
  Uptime:        2h 14m
  PID:           12345
  Memory:        45 MB

Listening:
  Proxy:         127.0.0.1:8402
  Management:    127.0.0.1:8403

Connected agents:
  • OpenClaw       ✓ active
  • Claude Code    ⚠ requires new terminal (env var not loaded)

Current preset:  Balanced
Requests today:  247 routed (saved $4.32 vs baseline)
```

If daemon is not running: print error and exit 1.

### 3.3 `krouter shell-init`

Output shell integration code. Used in `.zshrc` etc. via `eval`.

```bash
# In ~/.zshrc:
eval "$(krouter shell-init)"
```

Output for bash/zsh:
```bash
export ANTHROPIC_BASE_URL="http://localhost:8402"
export OPENAI_BASE_URL="http://localhost:8402/v1"
```

Output for fish:
```fish
set -gx ANTHROPIC_BASE_URL "http://localhost:8402"
set -gx OPENAI_BASE_URL "http://localhost:8402/v1"
```

Shell detection: `$SHELL` env var, fallback to `$0`. Output to stdout, no
newlines other than between statements.

### 3.4 `krouter config`

```
$ krouter config list
preset:           balanced
language:         en
notification_categories:
  free_credit:      true
  kinthai_product:  true
  provider_news:    true
  tip:              false

$ krouter config get preset
balanced

$ krouter config set preset saver
✓ Preset updated to saver (effective immediately)
```

### 3.5 `krouter budget`

```
$ krouter budget

Anthropic Pro Plan
────────────────────────────────────────
5h Window      ████████░░  35K / 44K   (4h 27m left)
Weekly Limit   ███░░░░░░░  92K / 308K  (3d 12h left)
Opus Quota     █████░░░░░  46K / 92K   (3d 12h left)

This week (so far):
  Routed:    1,247 requests
  Saved:     $8.42 vs no-routing baseline
```

ASCII bar chart. Use 10-char width.

### 3.6 `krouter logs`

```
$ krouter logs --lines=10
[2026-05-12 14:23:01] openclaw → anthropic/claude-sonnet-4-5  (4.2K in / 1.1K out / $0.024 / 1.8s)
[2026-05-12 14:23:45] cursor   → deepseek/deepseek-chat        (0.8K in / 0.3K out / $0.0001 / 0.4s)
[2026-05-12 14:24:12] openclaw → anthropic/claude-haiku        (2.1K in / 0.5K out / $0.003 / 0.9s)
...

$ krouter logs --follow
(streams new requests as they happen, Ctrl+C to exit)
```

### 3.7 `krouter test`

```
$ krouter test

Sending test request to verify routing...
  ✓ Proxy reachable at 127.0.0.1:8402
  ✓ Routing decided: anthropic/claude-haiku (Saver preset)
  ✓ Upstream response: 200 OK
  ✓ Response time: 312ms

End-to-end test PASSED. Your routing is working correctly.
```

If any step fails: clear diagnostic, exit 1.

### 3.8 `krouter version`

```
$ krouter version
krouter 1.0.0 (built 2026-05-10T14:23:01Z)
```

### 3.9 `krouter remote` (spec 10)

```
$ krouter remote enable
  ✓ Management port switched to 0.0.0.0:8403
  ✓ Self-signed certificate generated at ~/.kinthai/cert.pem
  ⚠ Please allow port 8403 in your firewall (one-time OS prompt)

$ krouter remote disable
  ✓ Management port back to 127.0.0.1:8403
  Note: 2 paired devices remain registered.
        Their tokens stay valid for next time remote is enabled.
```

### 3.10 `krouter pair`

```
$ krouter pair show
Pairing token:  KR-2KSF-9G7X-AMVR-83NK
Expires in:     4 min 23 sec

Watching for new tokens... (Ctrl+C to exit)
[refreshes when token rotates]

$ krouter pair devices list
ID         NAME           PAIRED AT       LAST SEEN
dev_001    MacBook Pro    2026-05-12      2 hours ago
dev_002    iPad Pro       2026-05-11      1 day ago

$ krouter pair devices revoke dev_001
✓ Device "MacBook Pro" revoked. Token invalidated immediately.
```

---

## 4. Output formatting

- TTY: ANSI colors, Unicode box drawing
- Non-TTY (piped/redirected): plain text, no colors

Use `golang.org/x/term.IsTerminal(os.Stdout.Fd())` for detection.

---

## 5. Exit codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Generic error |
| 2 | Daemon not running |
| 3 | Authentication failed (internal-token mismatch) |
| 4 | Command syntax error |

---

## 6. Test coverage

- Unit: each subcommand against mock daemon
- Unit: shell-init output for bash/zsh/fish
- Integration: spawn daemon + run CLI commands

---

## 7. Open questions

- Should `serve` daemonize itself (fork to background) when run interactively?
  No — LaunchAgent/systemd/Task Scheduler handles that. `serve` always runs
  in foreground. The OS service manager handles daemonization.
