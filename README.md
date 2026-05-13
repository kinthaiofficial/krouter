# krouter

> Local LLM proxy that saves you tokens. Runs as a background daemon, intercepts
> requests from AI agents (Claude Code, Cursor, OpenClaw), and routes them to
> the cheapest suitable provider — automatically.

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/go-1.22+-blue.svg)](go.mod)
[![Platform](https://img.shields.io/badge/platform-macOS%20%7C%20Linux%20%7C%20Windows-lightgrey)]()

---

## What it does

Your AI agent thinks it's talking to Anthropic / OpenAI / etc.
Behind the scenes, krouter:

- **Routes** to the cheapest provider that meets your quality bar
- **Tracks** cost per request, per model, per agent
- **Honors** your existing API keys (they never leave your machine)
- **Saves** you tokens — often 50-80% with the same outcomes

```
┌──────────────────────┐                  ┌──────────────────────┐
│  Claude Code /       │   localhost      │  krouter             │
│  Cursor / OpenClaw   │ ───────────────► │  daemon (8402)       │
└──────────────────────┘                  └──────────┬───────────┘
                                                     │
                          ┌──────────────────────────┼──────────────────────────┐
                          ↓                          ↓                          ↓
                  ┌──────────────┐          ┌──────────────┐          ┌──────────────┐
                  │  Anthropic   │          │   DeepSeek   │          │   Moonshot   │
                  └──────────────┘          └──────────────┘          └──────────────┘
                                          (router decides at each request)
```

## Highlights

- **Forever free, MIT licensed** — no paid tier, no subscription
- **Zero data upload** — no telemetry, no backend dependency
- **API keys never persisted** — they pass through, never touch disk
- **Zero-elevation install** — no `sudo`, no admin password
- **Cross-platform** — macOS, Linux, Windows
- **Same-protocol routing** — Anthropic requests stay Anthropic-protocol

## Supported providers

| Provider | Env var | Models |
|----------|---------|--------|
| Anthropic | `ANTHROPIC_API_KEY` | claude-3-5-sonnet, claude-3-haiku, ... |
| OpenAI | `OPENAI_API_KEY` | gpt-4o, gpt-4o-mini, ... |
| DeepSeek | `DEEPSEEK_API_KEY` | deepseek-chat, deepseek-reasoner |
| Groq | `GROQ_API_KEY` | llama-3.3-70b, mixtral-8x7b, ... |
| Moonshot | `MOONSHOT_API_KEY` | moonshot-v1-8k/32k/128k |
| GLM (Zhipu) | `ZHIPU_API_KEY` | glm-4, glm-4-flash, ... |
| Qwen (Alibaba) | `DASHSCOPE_API_KEY` | qwen-max, qwen-plus, qwen-turbo |

## Installation

> Pre-release builds are in development. Release packages will be available in Releases.

### macOS
```
1. Download krouter-X.X.X-macOS.dmg from Releases
2. Open the .dmg and drag to Applications
3. Launch — the install wizard handles the rest
```

### Windows
```
1. Download krouter-X.X.X-setup.exe from Releases
2. Run the installer (no admin required)
```

### Linux
```
# Debian/Ubuntu
sudo dpkg -i krouter_X.X.X_amd64.deb

# RPM-based
sudo rpm -i krouter-X.X.X.x86_64.rpm
```

## Quick start

After install, the wizard auto-detects and connects your AI agents. Then just use them as usual.

Set your provider API keys in your shell environment (or the GUI settings panel):

```sh
export ANTHROPIC_API_KEY=sk-ant-...
export DEEPSEEK_API_KEY=sk-...
```

## CLI

```sh
krouter status
krouter logs --follow
krouter budget
krouter config set preset saver
krouter test                          # smoke-test routing
```

## Building from source

Requires Go 1.22+, CGO enabled.

```sh
# Build daemon + CLI
make build

# Run tests
make test

# Lint
make lint

# Run in dev mode
make dev
```

For the GUI (`cmd/krouter-gui/`), [Wails v2](https://wails.io) is required:
```sh
make build-gui
```

## Privacy

The daemon:
- Sends requests **only** to LLM providers you configured (your keys, your providers)
- Polls `announcements.kinthai.ai/feed.json` every 6 hours (anonymous GET, no identifiers)
- Checks GitHub Releases every 24 hours for updates (anonymous GET)

We **never**:
- Upload any data to any backend
- Track user identifiers, emails, or usage patterns
- Store your API keys (they're forwarded from your env at request time)

## Architecture

Two processes:
- **`krouter` (daemon)** — runs as LaunchAgent / systemd --user / Windows Task Scheduler
- **`krouter-gui`** — Wails v2 desktop app, talks to daemon over HTTP

Two ports:
- **8402 (proxy)** — agent-facing, always 127.0.0.1, no auth
- **8403 (management)** — GUI-facing, 127.0.0.1 by default, optional 0.0.0.0 for LAN remote

## Contributing

PRs welcome. Please open an issue first for significant changes.

## License

MIT — see [LICENSE](LICENSE).

## Related projects

- [OpenClaw](https://github.com/kinthaiofficial/openclaw-kinthai) — managed OpenClaw fork
- [kinthai.ai](https://kinthai.ai) — agent marketplace
