# spec/04 — Agent Inheritance & 数据模型

> 状态：设计定稿 · 跨 v2.0.50 现状 + 后续 N 个版本的落地路线
> 作者：基于跨多轮架构讨论整理
> 关联文件：spec/02-routing-engine.md · spec/03-providers.md · spec/05-storage.md

---

## 1. 设计目标

让 krouter 在用户**已经在用 AI agent**（OpenClaw / Claude Code / Cursor / Cline / Codex / Hermes / …）的前提下，**自动接入并接管转发**，**零手动配置**。

差异化卖点：

> "其它 LLM 路由器要求你逐个改 agent 的 baseUrl 配置。krouter 装好之后自动识别你装了哪些 agent，一次性全部接管。"

---

## 2. 范围与边界

### 在范围内

- 扫描用户主目录下**各 AI agent 自身的配置文件**，提取 endpoint URL、API key、订阅 token 等
- Wizard / Dashboard 提供**默认路径展示 + 用户修改 + 重新扫描** 的统一 UX
- 把扫描结果写入 DB，daemon 运行时直接查 DB，零外部 manifest 文件解析

### 在范围外

- 不读用户 shell 环境变量（隐私敏感）
- 不扫 `~/.aws/credentials`、`~/.config/gcloud/` 等系统级凭证
- 不主动嗅探用户未通过 Wizard / Dashboard 明确确认的路径
- AWS Bedrock / Azure OpenAI / Vertex AI 等企业云需要 sigv4/OAuth-SA 类复杂 auth 的，**Phase 3 才做**

---

## 3. 名词约定

| 术语 | 定义 |
| --- | --- |
| **Agent** | 用户在本机使用的 AI 客户端工具（OpenClaw / Claude Code / Cursor / 等） |
| **Vendor** | 上游 LLM 厂商（Anthropic / MiniMax / Z.AI / DeepSeek / OpenRouter / …） |
| **Endpoint** | 一组 (vendor, protocol, base_url) 三元组，是 routing 实际目标 |
| **Protocol** | wire-format：`anthropic` / `openai` / 未来的 `gemini` 等 |
| **Scanner** | krouter 代码层的"如何解析某 agent 配置文件"的函数实现 |
| **Inheritance** | 把 agent 配置里已存在的 vendor 信息提取到 krouter 运行时 |

**不要混淆 agent 和 vendor**：一个 agent (OpenClaw) 可以同时配多个 vendor (Anthropic + MiniMax)；一个 vendor (Anthropic) 也可以被多个 agent 共用。

---

## 4. 用户视角的 UX

### 4.1 Wizard 中的 Agent 选择步骤

```
┌── 4 / 5  设置 AI agent 自动接入 ─────────────────────┐
│                                                     │
│  krouter 自动检测并接入以下 AI agent                  │
│                                                     │
│  ☑ OpenClaw                                         │
│     ~/.openclaw/openclaw.json        [修改] [扫描]   │
│     已发现 · 4 vendors                              │
│                                                     │
│  ☑ Claude Code                                      │
│     ~/.zshrc                         [修改] [扫描]   │
│     已发现                                          │
│                                                     │
│  ☐ Cursor                                           │
│     ~/.cursor/settings.json          [修改] [扫描]   │
│     未发现 · 修改路径后点扫描                         │
│                                                     │
│  ☐ Cline                                            │
│     ~/.cline/config.json             [修改] [扫描]   │
│     未发现                                          │
│                                                     │
│  ⓘ 至少 1 个 agent 已勾选且发现配置才能继续           │
│                                                     │
│           [全部重新扫描]  [下一步 →]                 │
└─────────────────────────────────────────────────────┘
```

**交互规则**：

1. 每个 agent 一行，含：勾选框 / display name / 当前路径 / 操作按钮 / 状态
2. **勾选** = 是否纳入 krouter 智能路由（用户可手动取消，即使已扫到也不接入）
3. **修改** = 弹出输入框让用户改路径
4. **扫描** = 对该 agent 立即重新扫描
5. **全部重新扫描** = 对所有列出的 agent 重新扫描
6. **下一步按钮的灰色逻辑**：必须至少 1 个 agent 满足 (`enabled && configFileExists`)，否则灰色
7. **没有"跳过此步"按钮、没有"跳过单个 agent"按钮** —— 一个 agent 都没接入则 krouter 装上没意义

### 4.2 Dashboard 的 Agent 管理页

与 Wizard **复用同一个 React 组件**，UX 完全一致。Dashboard 多两件事：

- 列表底下可以"添加 agent"（用户某个 agent 没出现在默认列表，例如 krouter 还没支持的）
- 配置变更**立即生效**（不需要点"下一步"按钮）

### 4.3 配置变更的实时性

- 用户改路径 + 点扫描 → daemon 立即跑该 agent 的 Scanner → 更新 DB → 广播 SSE → UI 刷新
- 用户取消勾选 → 立即从 routing 候选移除该 agent 的 vendor，但保留 DB 记录（重新勾选可恢复）
- daemon 收到配置变更后 < 1 秒内反映到 routing 决策

---

## 5. 系统架构 — 三层

```
┌─────────────────────────────────────────────────────────┐
│  Layer 1: Go 代码 (随 binary 发布)                       │
│  ─────────────────────────────                          │
│  internal/agentscan/scanner.go     Scanner interface    │
│  internal/agentscan/openclaw.go    OpenClawScanner      │
│  internal/agentscan/claudecode.go  ClaudeCodeScanner    │
│  internal/agentscan/cursor.go      CursorScanner (P2)   │
│  ...                                                     │
│  internal/agentscan/registry.go    var Scanners = [...] │
│                                                          │
│  作用: "krouter 这个版本支持哪些 agent + 各自默认路径"    │
│  加新 agent = 加 Go 文件 + PR + 发版                     │
└─────────────────────────────────────────────────────────┘
                            │
                            │ API 暴露: GET /internal/agents/supported
                            ▼
┌─────────────────────────────────────────────────────────┐
│  Layer 2: SQLite DB (运行时唯一数据源)                   │
│  ─────────────────────────────                          │
│  agent_settings        用户的勾选 + 路径 override        │
│  inherited_endpoints   扫描得到的 vendor endpoint        │
│                                                          │
│  ── 关键性质 ──                                          │
│  · DB 不预填,只有用户操作过才写入                         │
│  · daemon 启动只 READ,不预填默认值                       │
│  · 升级新版后新 Scanner 自动出现在 UI(从 Layer 1 拿),    │
│    不污染 DB                                             │
└─────────────────────────────────────────────────────────┘
                            │
                            │ API 暴露: GET /internal/agents/configured
                            │           POST /internal/agents/<id>/rescan
                            ▼
┌─────────────────────────────────────────────────────────┐
│  Layer 3: UI 组件 (Wizard / Dashboard 复用)              │
│  ─────────────────────────────                          │
│  并行调 supported + configured 两个 API,合并展示          │
│  - 已配过(DB 有) → 显示用户设置                          │
│  - 支持但未配(DB 无) → 显示 Scanner 的默认值,enabled=false │
└─────────────────────────────────────────────────────────┘
```

**没有第四层"用配置文件描述怎么解析配置文件"的元抽象**。Scanner 就是 Go 函数。

---

## 6. Scanner 代码层规范

### 6.1 Interface

```go
package agentscan

type Scanner interface {
    AgentID() string            // "openclaw" / "claude-code" / ...
    DisplayName() string        // "OpenClaw" / "Claude Code"
    DefaultConfigPath() string  // 平台相关的默认绝对路径
    Scan(configPath string) ([]InheritedEndpoint, error)
}

type InheritedEndpoint struct {
    Provider     string  // "anthropic" / "minimax-portal" / "openrouter" / ...
    EndpointURL  string  // 完整的上游 base URL 或本机 proxyBase
    ProtocolHint string  // "anthropic-messages" / "openai-chat" 等(可空)
    APIKey       string  // 直接存 key 字符串(file perm 0600 保护)
    ExtrasJSON   string  // 灵活字段: OAuth token / 订阅信息 / 自定义 header
}
```

### 6.2 Registry

```go
// internal/agentscan/registry.go
var Scanners = []Scanner{
    OpenClawScanner{},
    ClaudeCodeScanner{},
    // 后续按 PR 加: CursorScanner{}, ClineScanner{}, ...
}
```

### 6.3 单个 Scanner 实现示例

```go
// internal/agentscan/openclaw.go
type OpenClawScanner struct{}

func (OpenClawScanner) AgentID() string     { return "openclaw" }
func (OpenClawScanner) DisplayName() string { return "OpenClaw" }
func (OpenClawScanner) DefaultConfigPath() string {
    home, _ := os.UserHomeDir()
    return filepath.Join(home, ".openclaw", "openclaw.json")
}

func (OpenClawScanner) Scan(configPath string) ([]InheritedEndpoint, error) {
    data, err := os.ReadFile(configPath)
    if err != nil {
        return nil, fmt.Errorf("read openclaw config: %w", err)
    }
    // 解析 JSON
    // 遍历 models.providers
    // 顺便读 ~/.openclaw/agents/main/agent/auth-profiles.json 拿 OAuth token
    // 返回 InheritedEndpoint[]
}
```

### 6.4 失败原则

- Scanner 内部失败 → 返回 error，**绝不 panic**
- daemon 调 Scanner 出错 → 把 error 写入 `agent_settings.last_error`，UI 显示
- 一个 Scanner 失败**绝不影响**其它 Scanner 或 daemon 本身运行

---

## 7. 数据模型（DB schema）

### 7.1 agent_settings — 用户的接入选择

```sql
CREATE TABLE agent_settings (
    agent_id        TEXT PRIMARY KEY,        -- 'openclaw' / 'claude-code' / 'cursor' / ...
    enabled         INTEGER NOT NULL DEFAULT 0,    -- 用户勾选 = 1
    config_path     TEXT NOT NULL,           -- 用户改后的路径,或默认路径
    last_scanned_at INTEGER,
    last_error      TEXT                     -- 上次扫描的错误信息(UI 展示)
);
```

**特性**：

- DB **不预填**：daemon 启动**不写**这张表的默认值
- 只有用户在 wizard / dashboard 上点过的 agent 才有行
- 用户取消勾选 → `enabled = 0`，保留行，可重新勾选恢复
- 用户删除一行 → 完全恢复未配置状态

### 7.2 inherited_endpoints — 扫描结果

```sql
CREATE TABLE inherited_endpoints (
    agent_id        TEXT NOT NULL,
    provider        TEXT NOT NULL,
    endpoint_url    TEXT NOT NULL,
    protocol_hint   TEXT,
    api_key         TEXT,
    extras_json     TEXT,                    -- 灵活字段
    captured_at     INTEGER NOT NULL,
    PRIMARY KEY (agent_id, provider),
    FOREIGN KEY (agent_id) REFERENCES agent_settings(agent_id) ON DELETE CASCADE
);
```

**写入时机**：

- 用户在 Wizard / Dashboard 触发 rescan
- daemon 启动时对每个 `enabled = 1` 的 agent 自动 rescan 一次

---

## 8. API

### 8.1 GET /internal/agents/supported

返回 Scanner 注册表内容（来自 Go 代码，每次调用现取）：

```json
[
  {"agent_id": "openclaw",    "display_name": "OpenClaw",     "default_path": "/Users/frank/.openclaw/openclaw.json"},
  {"agent_id": "claude-code", "display_name": "Claude Code",  "default_path": "/Users/frank/.zshrc"},
  ...
]
```

### 8.2 GET /internal/agents/configured

返回 `agent_settings` 表 + 关联的 inherited_endpoints 计数：

```json
[
  {
    "agent_id": "openclaw",
    "enabled": true,
    "config_path": "/Users/frank/.openclaw/openclaw.json",
    "last_scanned_at": "2026-05-21T01:23:45Z",
    "last_error": null,
    "inherited_count": 4
  },
  ...
]
```

### 8.3 POST /internal/agents/{agent_id}/rescan

```
body: { "path": "/optional/custom/path" }
```

- 写 `agent_settings.config_path = path`（或默认）
- 调 `Scanners[agent_id].Scan(path)`
- 成功 → 写 `inherited_endpoints` (replace by agent_id)，更新 `agent_settings.last_scanned_at`
- 失败 → 更新 `agent_settings.last_error`
- 广播 SSE `"agents_changed"`

### 8.4 POST /internal/agents/{agent_id}/enable | /disable

切换 `agent_settings.enabled`，广播 SSE 让 routing engine 即时刷新候选 endpoint。

### 8.5 DELETE /internal/agents/{agent_id}

删除 `agent_settings` 那一行 + cascade 删 `inherited_endpoints`。

### 8.6 GET /internal/agents

返回 Layer 1 + Layer 2 合并视图，UI 一次拿到所有渲染需要的数据：

```json
[
  {
    "agent_id":     "openclaw",
    "display_name": "OpenClaw",
    "default_path": "/Users/frank/.openclaw/openclaw.json",
    "config_path":  "/Users/frank/.openclaw/openclaw.json",
    "enabled":      true,
    "last_scanned_at": "...",
    "last_error":   null,
    "inherited":    [
      {"provider": "anthropic",      "endpoint_url": "http://...", "has_key": true},
      {"provider": "minimax-portal", "endpoint_url": "http://...", "has_key": true,
       "extras_summary": "OAuth subscription token detected"}
    ]
  },
  {
    "agent_id":     "cursor",
    "display_name": "Cursor",
    "default_path": "/Users/frank/.cursor/settings.json",
    "config_path":  "/Users/frank/.cursor/settings.json",   ← 取自 default_path,因为 DB 无记录
    "enabled":      false,                                  ← DB 无记录默认 false
    "last_scanned_at": null,
    "last_error":   null,
    "inherited":    []
  },
  ...
]
```

---

## 9. 关联的"自动获取"数据基础设施

inheritance 是核心新增，但 daemon 还有三个**已经实装**的自动数据机制需要清晰说明：

### 9.1 ✅ pricing.json 从 LiteLLM 同步（已实现）

**当前状态（v2.0.50）**：

- 代码：`internal/pricing/pricing.go`
- 数据源：`https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json`
- 同步：`StartSync(ctx, 24*time.Hour)` daemon 启动后 24h 周期 + ETag conditional fetch
- 写入：`pricing_cache` 表（migration 001）
- 用途：routing engine `cheapestProviderModel()` 按 cost 排序候选

**保持不变**。

### 9.2 ✅ model_discovery 自动 pull /v1/models（已实现）

**当前状态（v2.0.50）**：

- 表：`model_discovery` (migration 003)
- 触发：
  - daemon 启动 10s 后 `RefreshModelsIfStale` 一次性扫所有 enabled provider
  - 用户在 Wizard / Dashboard 触发的 connect 流程也会刷新
  - `POST /internal/models/refresh` 手动触发
- 代码：`internal/api/server.go::discoverProviderModels` + 各 adapter 的 `DiscoverModels()`

**需要调整**：

- 当前 `discoverProviderModels` 用 `settings.ProviderKeys[name]` 拿 key，应改成**优先从 `inherited_endpoints.api_key` 拿**，让用户在 agent 里配的 key 自动被 daemon 用来 discover

### 9.3 ✅ subscription_quota 自动 poll（已实现）

**当前状态（v2.0.50）**：

- 表：`subscription_quota_cache` (migration 006)
- 代码：`internal/providers/minimax/quota.go::QuotaPoller`
- 数据源：`https://api.minimaxi.com/v1/token_plan/remains`（OAuth 鉴权）
- 同步：30 分钟 / 临近窗口重置时 5 分钟
- 用途：routing engine `SubscriptionSource` 让有套餐余量的 minimax-portal 优先

**需要调整**：

- 当前 OAuth token 是从 proxied 请求 header 捕获缓存的。改造后**优先从 `inherited_endpoints.extras_json` 里读 minimax OAuth token**（已经由 Scanner 抓出来了）

### 9.4 ❌ model_catalog 表（待删）

**当前状态**：表存在但实际**没数据**（LiteLLM ETag 304 跳过写入）。

**结论**：本架构定下的"用户已配好 agent → krouter 自动 inherit"前提下，**不需要这张表**。

- 用户 inherit 之后 `model_discovery` 已经有最权威的 model 列表（来自 `/v1/models`）
- LiteLLM 那 2720 行只在"用户还没配任何 key 也想看 vendor 上有哪些 model"的引导场景才有用，本场景不需要

**操作**：v2.0.51 或后续版本删除 migration 004 创建的 `model_catalog` 表、`internal/storage/model_catalog.go`、`pricing.OnSync` 推 catalog 到 adapter 的代码路径。`pricing_cache` 表保留（pricing 数据仍然要）。

---

## 10. 与现有代码的关系（v2.0.50 status quo）

### 10.1 已实现，符合本 spec 设计，**保留**

| 模块 | 文件 | 现状 |
| --- | --- | --- |
| pricing 同步 | `internal/pricing/pricing.go` | ✅ |
| model_discovery 表 + auto-pull | `internal/api/server.go::discoverProviderModels` 等 | ✅ |
| subscription_quota 表 + poller | `internal/providers/minimax/quota.go` | ✅ |
| `provider_config` 表 + POST /internal/providers | migration 007 + `loadProvidersFromDB` | ✅ 用作 vendor metadata；与本 spec 的 `inherited_endpoints` 互补 |
| LaunchAgent env / proxy auto-detect | `internal/proxycfg/proxycfg.go` 等 | ✅ |

### 10.2 已实现但**需要改造**

| 模块 | 现状 | 改造方向 |
| --- | --- | --- |
| `internal/config/agent_openclaw.go` | ConnectOpenClaw / DisconnectOpenClaw / ReadOpenClawProviderNames | 部分逻辑挪到 `internal/agentscan/openclaw.go::Scan()` 里，统一返回 `InheritedEndpoint[]` |
| `internal/config/agent_claudecode.go` | Connect/Disconnect/IsConnected | 同上：抽出 Scanner 实现 |
| `internal/config/agent_cursor.go` `agent_hermes.go` | Connect/Disconnect 半成品 | 同上，并补 `Scan()` 实现 |
| `internal/config/detect.go::DetectInstalledAgents` | hardcoded path 列表 | 删除，替换为 `agentscan.Scanners` 遍历 |
| `discoverProviderModels` 拿 key 的逻辑 | 用 `settings.ProviderKeys` | 改成优先用 `inherited_endpoints.api_key` |
| minimax `QuotaPoller` 拿 OAuth token | 从 proxied 请求 header 捕获 | 改成优先用 `inherited_endpoints.extras_json` 里的 OAuth token |

### 10.3 待新增

| 模块 | 文件 |
| --- | --- |
| Scanner interface + registry | `internal/agentscan/scanner.go`, `internal/agentscan/registry.go` |
| `agent_settings` 表 | migration 008 |
| `inherited_endpoints` 表 | migration 009 |
| 新 API endpoints (§8) | `internal/api/server.go::handleAgentXxx` |
| Wizard "Agent Paths" step | `frontend-install/src/pages/AgentPathsStep.tsx` |
| Dashboard Settings → Agents 页 | `frontend/src/pages/Agents.tsx`（已有，需扩展） |
| SSE event `"agents_changed"` | broadcast 协议扩展 |

### 10.4 待删除

| 模块 | 备注 |
| --- | --- |
| `internal/storage/migrations/004_model_catalog.sql` | 表删除（drop migration 010）|
| `internal/storage/model_catalog.go` | 文件删除 |
| `pricing.OnSync` 推 catalog 到 adapter 的代码路径 | `cmd/krouter/serve.go` 相应回调清理 |

---

## 11. 数据流总图

```
                                  ┌── Wizard / Dashboard UI ──┐
                                  │                            │
                                  │  GET supported  (Go layer) │
                                  │  GET configured (DB layer) │
                                  │  POST rescan               │
                                  │  POST enable/disable       │
                                  └─────────┬──────────────────┘
                                            │
                                            ▼
                          ┌─────────────────────────────────────┐
                          │ daemon                              │
                          │                                     │
                          │ agentscan.Scanners (Go code)        │
                          │   ↓ on rescan trigger               │
                          │ Scanner.Scan(configPath)            │
                          │   → InheritedEndpoint[]             │
                          │   ↓                                 │
                          │ DB: inherited_endpoints, agent_set  │
                          │   ↓                                 │
                          │ broadcast SSE "agents_changed"       │
                          └─────────────────┬───────────────────┘
                                            │
                                            ▼
                          ┌─────────────────────────────────────┐
                          │ Routing decision (per request)      │
                          │                                     │
                          │ 1. SELECT * FROM inherited_endpoints │
                          │    WHERE agent_id IN (enabled set)   │
                          │ 2. 协议匹配 + cost (pricing_cache) +  │
                          │    quota (subscription_quota_cache)   │
                          │ 3. discovery (model_discovery 表) 决  │
                          │    定该 endpoint 是否真支持该 model    │
                          │ 4. forward 到 endpoint_url            │
                          │    带上 api_key 或 OAuth token         │
                          └─────────────────────────────────────┘

  独立后台进程,与上面 inheritance 流程并行:
  ─────────────────────────────────────
  pricing.StartSync         24h LiteLLM ETag → pricing_cache
  minimax.QuotaPoller       30min OAuth → subscription_quota_cache
  RefreshModelsIfStale      24h /v1/models per endpoint → model_discovery
```

---

## 12. 用户配置数据的来源责任

| 数据 | 谁维护 | 频率 |
| --- | --- | --- |
| pricing 信息 | LiteLLM 上游 + krouter daemon 自动同步 | 24h ETag |
| 每个 endpoint 的可用 model | endpoint 自家 `/v1/models` + krouter daemon 自动拉 | 24h |
| 订阅 quota（minimax 等） | endpoint 自家 quota API + daemon 自动 poll | 30min |
| 各 agent 的配置文件位置（默认路径）| **krouter 代码层**（Scanner.DefaultConfigPath）| 发版时 |
| 各 agent 的配置 schema 解析逻辑 | **krouter 代码层**（Scanner.Scan）| 发版时 |
| 用户的"勾选哪个 agent / 改了什么路径" | **DB**（用户操作写入）| 用户操作时 |
| 用户的 API key / OAuth token | **agent 配置文件**（用户在 agent 里早就配好）| 用户配 agent 时 |

**用户什么都不用单独填给 krouter**。Wizard 唯一的人机交互就是"看看默认路径对不对，必要时改一下，勾选要接入的 agent"。

---

## 13. 路径覆盖与失败处理

### 13.1 路径解析两层

```
用户 override (agent_settings.config_path)       ← 优先
       ↓ 没改
Scanner.DefaultConfigPath() (Go 代码常量)        ← 默认
       ↓ 文件不存在
UI 显示 "未发现", 路径输入框等用户改
```

**不做兜底扫描**（不试 homebrew / npm-global 等位置）。Scanner 的默认路径就是该 agent 官方文档约定的位置。其它位置由用户自己改路径告知。

### 13.2 失败兜底

| 情况 | 处理 |
| --- | --- |
| Scanner 不在注册表 | UI 不显示该 agent |
| config_path 不存在 | `last_error = "config file not found"`，UI 显示"未发现"，路径输入框可改 |
| 文件存在但 Scanner 解析失败 | `last_error = error message`，UI 显示"配置疑似格式异常"，daemon 继续跑 |
| 字段部分缺失 | 部分 `inherited_endpoints` 写入，缺失字段 UI 标灰 |
| 一个 Scanner panic | recover 捕获，写 error，**绝不影响**其它 Scanner 或 daemon 本体 |

---

## 14. 实施阶段

### Phase 1（1-1.5 周，可发布到 v2.1.x）

P1 目标：架构落地，至少 OpenClaw + Claude Code 跑通。

- [ ] migration 008：`agent_settings` 表
- [ ] migration 009：`inherited_endpoints` 表
- [ ] `internal/agentscan/` 包：Scanner interface + registry
- [ ] `internal/agentscan/openclaw.go`：从 `agent_openclaw.go` 抽 Scanner 实现
- [ ] `internal/agentscan/claudecode.go`：从 `agent_claudecode.go` 抽
- [ ] 新 API endpoints (§8)
- [ ] daemon 启动时对 enabled agent 自动 rescan
- [ ] Wizard 加 Agent Paths step
- [ ] Dashboard Agents 页升级到 Wizard 同款组件
- [ ] SSE `"agents_changed"` 事件
- [ ] 旧的 `ConnectOpenClaw` / `DetectInstalledAgents` 调用方迁移到新 API
- [ ] `discoverProviderModels` 优先用 `inherited_endpoints.api_key`
- [ ] minimax `QuotaPoller` 优先用 `inherited_endpoints.extras_json` 里的 OAuth

### Phase 2（每个 agent 独立 PR，各 0.5-1 天）

P2 目标：扩展 Scanner 注册表覆盖常见工具。

- [ ] `internal/agentscan/cursor.go` — Scan `~/.cursor/settings.json`
- [ ] `internal/agentscan/cline.go`
- [ ] `internal/agentscan/codex.go`
- [ ] `internal/agentscan/hermes.go`

### Phase 3（按用户需求，各 1-2 周）

P3 目标：覆盖企业云 vendor，等用户提需求再做。

- [ ] AWS Bedrock 支持（需新 auth_type: aws_sigv4）
- [ ] Azure OpenAI 支持（需 url template + api-key header）
- [ ] Vertex AI 支持（需 OAuth via service account）

### Phase 4（cleanup）

- [ ] 删 `model_catalog` 表 + 相关代码（migration 010 drop）
- [ ] 删过时的 `internal/config/detect.go::DetectInstalledAgents` 等旧路径

---

## 15. 已确认的设计取舍记录

供后续 review 时回顾,避免重新讨论:

| 取舍 | 决定 | 理由 |
| --- | --- | --- |
| 是否做"全自动 inheritance" | ✅ 做 | krouter 核心差异化卖点,胜过同类工具 |
| 是否扫 shell env | ❌ 不做 | 隐私敏感 |
| 是否扫 `~/.aws/credentials` 等系统配置 | ❌ 不做 | Phase 3 用户提需求时再考虑 |
| 是否做 `inheritance_rules.json` 这种数据驱动的 Scanner | ❌ 不做 | 过度抽象,Scanner 写 Go 代码足够清晰 |
| 是否做 path 兜底自动扫(homebrew/npm-global 等) | ❌ 不做 | Scanner 只声明官方默认路径,其它用户自己改 |
| Wizard 是否有"跳过此步" | ❌ 不做 | 强制至少 1 个 agent 接入,否则装 krouter 没意义 |
| Wizard 是否有"跳过单个 agent" | ❌ 不做 | 没扫到 = 用户没装该 agent,语义上自洽 |
| 默认路径写在哪 | Go 代码 (Scanner.DefaultConfigPath) | 不是 DB 也不是 JSON;DB 只存用户偏好 |
| DB 是否预填所有可能 agent 的默认行 | ❌ 不做 | DB 只反映用户操作,daemon 启动只 READ |
| 是否保留 `model_catalog` 表 | ❌ 删 | 用户已配 agent 场景下完全冗余 |
| pricing/discovery/quota 是否动 | ❌ 不动 | 已实装且符合设计,仅调整 key 来源用 `inherited_endpoints` |

---

## 16. 产品话术（marketing 参考）

```
其它 LLM 路由的体验:
  装好后 → 读各 agent 文档 → 一个个改 baseUrl → 试请求 → debug

krouter 的体验:
  装好之后,Wizard 弹出来:
  "检测到你已装 OpenClaw, Claude Code, Cursor — 全部一键接管"
  下一步 → 完成

差异不是功能多少,是配置成本.
```
