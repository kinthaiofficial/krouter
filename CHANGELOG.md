# Changelog

All notable changes to krouter will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [2.0.32] - 2026-05-18

### Fixed
- **MiniMax 请求计费**：`MiniMax-M2.7` 和 `MiniMax-M2.7-highspeed` 加入静态价格表（LiteLLM 目前未收录 MiniMax 模型，导致 `cost_micro_usd = 0`）。价格来源：MiniMax API 平台（$0.30/1M input、$1.20/1M output）
- **Savings 误算**：当 `cost_micro_usd = 0`（未知模型价格，非真正免费）时，savings 会被错误地计为 `baseline - 0 = baseline`。现在仅在实际费用 `> 0` 时才计算 savings，影响 `/internal/budget` 和 `/internal/usage` 两个端点
- **OpenClaw agent 识别**：OpenClaw 使用 Anthropic TypeScript SDK，其 User-Agent 为 `Anthropic/JS X.Y.Z`，不含 "openclaw" 字符串，导致 `agent = "unknown"`。新增通过 `anthropic-dangerous-direct-browser-access: true` 请求头识别 OpenClaw（该 header 由 OpenClaw SDK client 在所有 Anthropic provider 请求中注入，Claude Code CLI 不发送此 header）
- **per-agent 统计**：随 agent 识别修复自动生效，OpenClaw 请求将正确计入 `openclaw` 分组

## [2.0.31] - 2026-05-18

### Fixed
- **MiniMax-portal OAuth 透传**：v2.0.30 错误地将 MiniMax adapter 改成 Bearer key 注入模式。MiniMax-portal 的认证由 OpenClaw 的 OAuth 流程生成（从 `auth-profiles.json` 读取 access token），krouter 不应注入任何 key，应完整透传 OpenClaw 生成的 Authorization header。MiniMax adapter 恢复为 Anthropic 协议透明代理，upstream 端点更新为正确的 `https://api.minimaxi.com/anthropic`（portal API，区别于 chat API `api.minimax.chat`）
- **`ConnectOpenClaw` 新增 `minimax-portal` 支持**：若用户 openclaw.json 中有 `minimax-portal` provider（含 `authHeader: true` OAuth 配置），connect 时自动将其 `baseUrl` 重定向到 krouter；disconnect 时恢复为 `https://api.minimaxi.com/anthropic/v1`。OAuth 凭证（`authHeader`、models、access token 等）全程不接触

### Architecture note
MiniMax-portal 的完整流程：
1. OpenClaw → krouter:8402（OpenClaw OAuth 流程注入 Authorization header）
2. krouter → `https://api.minimaxi.com/anthropic/v1/messages`（透明代理，保留原始 header）

## [2.0.30] - 2026-05-18

### Fixed
- **LaunchAgent 无 `EnvironmentVariables`**：plist 新增 `EnvironmentVariables` 段，注入扩展 `PATH`（含 `~/.claude/local`、`~/.local/bin`、`~/go/bin`、`/opt/homebrew/bin`、`/usr/local/bin`）和 `HOME`，彻底解决 daemon 作为 LaunchAgent 运行时工具不可见的问题
- **Provider 注册依赖 shell env**：所有 secondary provider（DeepSeek / Groq / Moonshot / GLM / Qwen / MiniMax）改为**始终注册**；API key 通过 `keyFn` 在每次请求时动态读取，优先读 `~/.kinthai/settings.json` 的 `provider_keys`，再 fallback 到环境变量。LaunchAgent 无 shell env 时仍可通过 settings.json 配置 key
- **Providers 页只显示 anthropic**：`GET /internal/providers` 新增 `configured: bool` 字段；有 key 的 provider 显示为 "Active"，无 key 的显示为 "Not configured"（不再从注册表缺失，始终可见）
- **Claude Code 在 Agents 页永远空**：`DetectInstalledAgents` 在 `exec.LookPath("claude")` 失败后改为搜索已知路径（`~/.claude/local/claude`、`~/.local/bin/claude`、`/usr/local/bin/claude`、`/opt/homebrew/bin/claude`），LaunchAgent 最小 PATH 下也能找到
- MiniMax adapter 从 Anthropic 透明代理改为 OpenAI Bearer-auth 方式，key 注入逻辑与其他 secondary provider 一致

### Added
- `Settings.ProviderKeys map[string]string`：存储 secondary provider 的 API key，存入 `~/.kinthai/settings.json`（0600 权限）；`PATCH /internal/settings` 支持 `provider_keys` 字段（空字符串 = 删除该 key）
- `providers.Configurable` 可选接口：`HasKey() bool`，由 `openai.Adapter` 实现；透明代理（Anthropic）不实现，默认视为已配置
- `openaiAdapter.NewWithKeyFn` / `NewWithPathReplaceAndKeyFn`：接受 `func() string` 的 key getter，替代硬编码环境变量名
- 所有 secondary adapter（deepseek / groq / moonshot / glm / qwen / minimax）加 `NewWithKeyFn` 构造函数
- Providers 页 "Not configured" 卡片 hint 更新为同时提示 env var 和 settings.json 两种配置方式

## [2.0.29] - 2026-05-18

### Fixed
- **OpenClaw connect 后仍 crash-loop（Bug C）**：`ConnectOpenClaw` 写入配置时未设置 `models.providers.anthropic.models` 字段，但 OpenClaw schema 强校验该字段必须为非空数组，导致 OpenClaw 启动即崩溃。现在连接时若 `models` 字段缺失，自动注入当前生产 Claude 模型列表；用户已有的自定义 models 列表不覆盖
- **`DisconnectOpenClaw` 遗留垃圾节点**：旧 krouter 版本断开后会在 `models.providers` 下残留空的或仅有 `models` 字段的 `anthropic` 对象。现在：若断开后 provider 中不存在真实 `apiKey`，说明该 anthropic section 完全由 krouter 创建，一并清除（包括 krouter 注入的 `models` 数组和空节点）
- 完善 `TestDisconnectOpenClaw_RemovesOldPlaceholderApiKey`：占位符 apiKey 被移除后，因无真实 key 留存，整个 anthropic section 一并删除

### Added
- 新增测试 `TestConnectOpenClaw_PreservesExistingModels`：确认用户已有 models 列表不被覆盖
- 新增测试 `TestDisconnectOpenClaw_RemovesKrouterAddedSectionWhenNoRealKey`：无真实 apiKey 时整个节点被清除
- 新增测试 `TestDisconnectOpenClaw_PreservesRealApiKeyAndCustomModels`：用户真实 apiKey 和自定义 models 在断开后完整保留

## [2.0.28] - 2026-05-18

### Added
- **Logs 页 SSE 实时追加**：`request_completed` 事件直接推送到 Logs 页，新请求无需等待轮询即时出现
  - 单个稳定 SSE 连接；切换 agent 过滤器时通过 ref 避免 stale closure
  - 内存上限 2000 条；超出后自动丢弃尾部
- **Logs 页 Agent 下拉过滤器**：选中 agent 后使用 `?agent=name` 后端参数，只拉该 agent 的记录
  - 下拉选项从 `/internal/agents` 动态读取
  - 文本搜索框与 agent 过滤器可叠加使用
- **`proxy → SSE` 接线**：`proxy.Server.SetOnComplete` 回调 + `serve.go` 接线，每次请求完成后立即广播 `request_completed` 事件（此前该事件类型存在但从未发送）
- **About 页显示 build_time**：`/internal/status` 响应新增 `build_time` 字段；About 页在版本信息卡片中展示构建时间（`unknown` 时不显示）

### Fixed
- `request_completed` SSE 事件此前已在 Dashboard 监听但后端从未广播，现补全发送逻辑

## [2.0.27] - 2026-05-18

### Added
- **Agents 独立页**（P0 gap 补全）
  - 左侧导航新增「Agents」入口（`Bot` 图标），位于 Dashboard 下方
  - 每个 agent 卡片显示：连接状态徽章、配置文件路径、provider 列表、今日请求数 / 今日费用 / 今日节省
  - **Connect / Disconnect 按钮**：直接修改 agent 配置文件（openclaw.json / settings.json / config.toml / shell rc），无需手动编辑
  - 连接后提示：Claude Code 需要打开新终端；OpenClaw / Cursor 需要重启
  - 展开 / 折叠 per-agent 请求日志（Show logs / Hide logs）
  - Re-detect 按钮强制重新扫描已安装 agent
- **`POST /internal/agents/{name}/connect`** / **`POST /internal/agents/{name}/disconnect`** 端点
  - 支持 `openclaw` / `cursor` / `hermes` / `claude-code`
  - 找不到 agent 返回 404；不支持的 agent 名返回 400
- **`GET /internal/logs?agent={name}`** 过滤参数，返回指定 agent 的最近请求记录
- **`GET /internal/agents`** 返回值新增 `stats` 字段（`requests_today` / `cost_today_usd` / `savings_today_usd`）
- `storage.ListRequestsByAgent(ctx, agent, limit)` — 带 agent WHERE 过滤的存储层方法
- `AgentInfo` 字段加 JSON tag（`name` / `config_path` / `cli_path`），修复之前 PascalCase 序列化 bug

### Changed
- Providers 页移除「AI Agents」子区块（内容已移至独立 Agents 页）

---

## [2.0.26] - 2026-05-18

### Fixed
- **OpenClaw config 写入逻辑双重 bug 修复**
  - **Bug A（crash loop）**：旧版 `ConnectOpenClaw` 向 `models.providers.anthropic.apiKey`
    写入字面量 `"${ANTHROPIC_API_KEY}"`。OpenClaw 作为 LaunchAgent 运行，不继承 shell
    环境，该占位符从不展开，直接作为无效 API key 发给 Anthropic，导致 OpenClaw
    crash loop。新版完全不写 `apiKey`——用户自己的真实 key 必须保留在 OpenClaw 配置里。
  - **Bug B（字段覆盖）**：旧版用 `setNestedJSON` 把整个 `anthropic` 节点替换成三个固定字段，
    销毁用户原有的所有配置（如 minimax apiKey、models 列表）。新版改为 merge：只写
    `baseUrl` 和 `api`，所有其他字段原样保留。
  - **旧版清理**：`DisconnectOpenClaw` 现在会识别并删除旧版写入的 `${ANTHROPIC_API_KEY}`
    占位符，修复升级路径；真实 apiKey 永远不会被 Disconnect 删除。
  - 5 个测试覆盖：baseUrl+api 设置、apiKey 不注入、backup 创建、disconnect 保留真实 key、
    disconnect 清除占位符

---

## [2.0.25] - 2026-05-18

### Added
- **`GET /internal/agents`** — 新 API 端点，实时探测已安装的 AI agent 并报告
  krouter 连接状态：
  - OpenClaw: 检查 `~/.openclaw/openclaw.json` 是否存在，`models.providers.anthropic.baseUrl`
    是否指向 `http://127.0.0.1:8402`，并列出配置文件中的所有 LLM provider 名称
    （如 anthropic、minimax）
  - Claude Code: 检查 `~/.zshrc` / `~/.bashrc` / `~/.bash_profile` 是否包含
    krouter shell integration marker block
  - Cursor / Hermes: 检测文件是否存在（连接状态检测后续实现）
- **Providers 页 "AI Agents" 区块** — 展示每个 agent 的探测结果、连接状态
  （Connected / Not connected 徽章）、配置文件路径、以及 agent 内部配置的
  LLM provider 列表；每 15 s 自动刷新
- `IsOpenClawConnected(configPath)` — 读取 OpenClaw 配置，判断是否已接入 krouter
- `ReadOpenClawProviderNames(configPath)` — 读取 OpenClaw 配置内所有 provider 名称
- `IsClaudeCodeConnected(shellRCPath)` — 检查 shell RC 是否包含 krouter marker
- `DetectAgentStatuses()` — 在 `DetectInstalledAgents()` 基础上补充连接状态和 provider 列表
- 8 个单元测试覆盖上述新函数

---

## [2.0.24] - 2026-05-18

### Changed
- **Dashboard UI 白色主题** — 参照 kinthai.ai 设计风格重新配色：
  背景色从 `#f7f8fa` 改为 `#f0f2f5`，字体改为系统 UI 栈（`-apple-system, BlinkMacSystemFont, ...`），
  强制 light 模式（移除全部 `dark:` Tailwind 类），logo 更新为 kinthai.ai 完整渐变 SVG。
- **Tab 标题** — 浏览器标签从 "frontend" 改为 "KRouter"；favicon 路径修正为 `/krouter/favicon.svg`
- **版本号 double-v 修复** — 侧边栏版本显示从 `v{version}` 改为 `{version}`，避免 git tag `v2.0.x`
  导致显示 `vv2.0.x`

---

## [2.0.23] - 2026-05-18

### Fixed
- **安装完成后 installer 进程自动退出** — `krouter-installer` 此前以 `select {}`
  永久阻塞，安装向导结束后进程一直存在，导致用户在 Activity Monitor 里看到两个
  "krouter" 进程（一个有图标的 installer、一个无图标的 daemon）。
  现在 `handleDaemonReady` 在返回 `ready: true` 后 500 ms 关闭一个 shutdown
  channel，`main.go` 阻塞在该 channel 而非 `select {}`，收到信号后正常退出。
  安装完成后用户只会看到一个 krouter daemon 进程。

---

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
