# spec/05 — 订阅类 vendor 的 quota 控制

> 状态：设计定稿
> 关联文件：spec/02-routing-engine.md · spec/04-agent-inheritance.md · spec/05-storage.md

---

## 1. 设计目标

让 krouter 在用户**已订阅**某 vendor 套餐时（如 MiniMax 5h 重置 1500 次套餐），routing 决策优先使用**已包月的剩余配额**（effective cost ≈ $0），用尽后才 fallback 到按 token 计费的候选。

> 月费均摊后单次 ≈ $0.000031，比任何 per-token provider 都便宜两个数量级。优先用订阅是显然的最优策略。

---

## 2. 范围与边界

### 在范围内

- vendor 提供 quota 查询 endpoint 的订阅模式（MiniMax 套餐是首个，未来其它类似模式可扩展）
- OAuth token 从 agent 配置文件 inherit（不让 krouter 持有用户凭证）
- 按 wildcard pattern 匹配 model id 到 billing tier
- Routing engine 在所有 preset（saver / balanced / quality）下都优先选订阅余量
- Dashboard UI 展示每个 tier 的余量、重置时间、来源

### 在范围外

- 不持久化用户 OAuth token 到 krouter 自己的 DB（仅运行时从 `inherited_endpoints.extras_json` 读）
- 不刷新 OAuth token 生命周期（由 user agent 自己处理）
- 不区分 per-feature quota（例如 coding-plan-search 单独计费）— Phase 3 才做
- 用户自己设置的 daily budget downgrade 不在本 spec — 那是 `applyQuotaDowngrade`（v2.0.45），与本机制并存但解决不同问题

---

## 3. 名词约定

| 术语 | 定义 |
| --- | --- |
| **Subscription quota** | vendor 按"次"售卖的配额，按时间窗口（5h / day / week）重置 |
| **Tier** | quota 的一个计费单位，可能是单 model 或 wildcard model 系列 |
| **Window** | 该 tier 的当前配额窗口（start_time → end_time） |
| **Effective cost** | 月费 ÷ 该套餐总额度 ÷ 平均请求长度，typically ≈ $0.000031 / 次 |
| **OAuth token** | vendor 对订阅用户颁发的认证凭证（如 MiniMax 的 sk-cp-... access token） |
| **User budget downgrade** | 用户在 settings 里设的 spending 上限触发的降级 — **本 spec 不涉及** |

---

## 4. 用户视角

### 4.1 Dashboard 上的展示

```
┌─ Subscription Quota ───────────────────────────────────┐
│                                                          │
│  MiniMax (via OpenClaw)                                 │
│                                                          │
│  MiniMax-M* (LLM 系列)            1479 / 1500 次         │
│  ████████████████████░░  本窗口剩 4h 28m                 │
│  下次重置: 2026-05-21 16:00 CST                          │
│                                                          │
│  speech-hd                        4000 / 4000 次         │
│  ████████████████████░░  日额度, 24:00 重置              │
│                                                          │
│  MiniMax-Hailuo-2.3-*             0 / 0 次               │
│  (未购买视频套餐)                                         │
│                                                          │
│  coding-plan-vlm                  150 / 150 次           │
│  ████████████████████░░  本窗口剩 4h 28m                 │
│                                                          │
│  ⓘ 来源: OpenClaw OAuth subscription                    │
│  ⓘ 最近刷新: 12 分钟前  [立即刷新]                       │
└────────────────────────────────────────────────────────┘
```

### 4.2 用户感知到的 routing 行为

- 当订阅 quota 充足：所有 MiniMax-M* 请求都走订阅，**用户不付任何额外 token 费**
- 当订阅 quota 耗尽：自动 fallback 到 pay-per-token vendor（按 token_price_api 选 cheapest），UI 顶部弹一条 toast "MiniMax 5h 配额已用尽，已切换到 DeepSeek（$0.04/MTok）"
- 窗口重置时刻自动恢复订阅优先

---

## 5. 系统架构

```
┌────────────────────────────────────────────────────────────────────┐
│ 数据流                                                              │
│                                                                     │
│  Scanner (per spec/04)                                              │
│    ↓ 读 ~/.openclaw/agents/main/agent/auth-profiles.json            │
│    ↓ 抽 minimax-portal:default.access (OAuth access token)          │
│    ↓                                                                │
│  inherited_endpoints.extras_json  (运行时, 不长期持久化)             │
│    { "oauth_token": "sk-cp-...", "purpose": "minimax_subscription" }│
│    ↓                                                                │
│  QuotaPoller (每 30 min / 临近重置 5 min)                          │
│    ↓ Authorization: Bearer <oauth_token>                            │
│    ↓ GET https://api.minimaxi.com/v1/token_plan/remains             │
│    ↓ 解析 base_resp.status_code (避免假 200)                         │
│    ↓                                                                │
│  subscription_quota_cache 表                                         │
│    ↓ provider + model_pattern + window + total/used + fetched_at    │
│    ↓                                                                │
│  SubscriptionSource interface (routing engine)                       │
│    ↓ 决策时 query                                                    │
│    ↓                                                                │
│  Routing decision                                                    │
│    Saver/Balanced/Quality 三个 preset 都先看订阅 quota 是否够        │
└────────────────────────────────────────────────────────────────────┘
```

---

## 6. 数据获取链路（详）

### 6.1 OAuth token 来源

```
来源优先级:
  1. inherited_endpoints.extras_json (Scanner 抓到的, 首选)
  2. 历史兼容: 从 proxied 请求 header 捕获 (v2.0.44 实现, 仅作 fallback)
  
Scanner 抓 token 的方式 (在 OpenClawScanner.Scan 内):
  - 配置文件路径: ${HOME}/.openclaw/agents/<agent_name>/agent/auth-profiles.json
  - JSONPath:     $.profiles["minimax-portal:default"].access
  - 仅当文件存在且 profile 是 OAuth type 时才抓
```

### 6.2 Token 不持久化原则

**OAuth token 是用户凭证,krouter 只在运行时 inherit 一份,不写入长期持久化**：

- `subscription_quota_cache` 表**只存 quota 数据**（次数、窗口、时间戳），**不存 token**
- `inherited_endpoints.extras_json` 视为短期运行缓存（用户在 OpenClaw 重新登录 → Scanner 重新抓 → 自动刷新）
- token 过期由 user agent（OpenClaw）自己处理，krouter 检测到 401 时主动触发该 agent 重新扫描

### 6.3 静态 API key 模式（minimax-portal 也支持）

如果用户在 OpenClaw 里配的是 minimax-portal 的 **static API key**（`apiKey` 字段，不是 OAuth），:

- Scanner 抓到 `inherited_endpoints.api_key` 而不是 `extras_json.oauth_token`
- QuotaPoller 检测到没有 OAuth → silent skip
- UI 标灰：「当前 minimax 是 static key 模式，无套餐数据可显示」
- routing 仍可正常用 minimax（透传 static key），只是没"订阅优先"加成

---

## 7. 数据模型（DB schema）

### 7.1 subscription_quota_cache（v2.0.44 已实装）

```sql
CREATE TABLE subscription_quota_cache (
    provider         TEXT NOT NULL,        -- "minimax" / 未来其它订阅 vendor
    model_pattern    TEXT NOT NULL,        -- "MiniMax-M*", "speech-hd", ...（wildcard or 具体名）
    window_start     INTEGER NOT NULL,     -- ms UTC
    window_end       INTEGER NOT NULL,     -- ms UTC
    total_count      INTEGER NOT NULL,     -- 该窗口总额度
    used_count       INTEGER NOT NULL,     -- 已用
    highspeed        INTEGER NOT NULL DEFAULT 0,  -- 1 = highspeed plan
    fetched_at       INTEGER NOT NULL,     -- 上次 poll 时间 ms UTC
    PRIMARY KEY (provider, model_pattern)
);
```

可读字段衍生：
- `remaining = total_count - used_count`
- `seconds_until_reset = (window_end - now) / 1000`
- `effective_cost_per_call = monthly_price_cny × cny_to_usd / (total_count × windows_per_month)` (see §11 for pricing derivation)

**Important**: `total_count` is the **quota for a single 5h window**, not a monthly quota. Monthly call total = `total_count × windows_per_month` where `windows_per_month = 144` (30 days × 24h ÷ 5h). An earlier draft of this spec missed the `× windows_per_month` factor, which caused PR #1 to ship two pricing tables that disagreed by a factor of ~1043. See the bug history at the end of §11.

### 7.2 不需要新表

`inherited_endpoints.extras_json` 已经能装 OAuth token（spec/04 定义），不需要为 token 单独表。

---

## 8. Tier 匹配 — wildcard pattern

vendor 返回的 tier 名字是 wildcard pattern，routing 时把请求 model id 展开匹配到 tier：

```
tier name (从 API 拿)         匹配的 model id 例子
─────────────────────────    ───────────────────────────────────
MiniMax-M*                   MiniMax-M2.7, MiniMax-M2.7-highspeed,
                             MiniMax-M2.5, MiniMax-M2.5-highspeed,
                             MiniMax-M2.1, MiniMax-M2.1-highspeed, MiniMax-M2
                             (覆盖 MiniMax-M 系列全部 LLM)

speech-hd                    speech-hd (具体名,不带 wildcard)

MiniMax-Hailuo-2.3-*         MiniMax-Hailuo-2.3-Fast-6s-768p,
                             MiniMax-Hailuo-2.3-6s-768p,
                             (视频系列)

coding-plan-search           (不是 model 名, 是 tool 使用计费,
                             Phase 3 才处理)
```

**匹配规则**：标准 glob 风格 `*`，daemon 用 Go `path.Match` 或等价实现。

**匹配优先级**：精确名 > wildcard。如果同 provider 下既有 `MiniMax-M*` 又有 `MiniMax-M2.7` 单独 tier（理论上 vendor 不会这样设计，但代码要鲁棒），具体名优先。

---

## 9. Routing 集成

### 9.1 SubscriptionSource interface

```go
package routing

type SubscriptionInfo struct {
    Provider       string
    MatchedTier    string         // 命中的 tier name
    Remaining      int64
    Total          int64
    SecondsToReset int64
    EffectiveCost  float64        // monthly_price / total
}

type SubscriptionSource interface {
    // 给一个 provider name 和 model id, 返回是否有可用订阅 quota
    LookupSubscription(provider, model string) (*SubscriptionInfo, bool)
}
```

`internal/api/server.go` 提供实现 (`newSubscriptionSource`)，从 `subscription_quota_cache` 表 + wildcard match 拿数据。

### 9.2 Engine 决策中的位置

```
Engine.Decide(req, preset):
  protocol = parse from path
  candidates = registry.providers(protocol)
  
  # 第 1 步: 找有订阅 quota 的候选 (跨 preset 通用)
  for ep in candidates:
    sub, ok := subscriptionSource.LookupSubscription(ep.Name, req.Model)
    if ok && sub.Remaining > 0:
      return Decision{
        Provider: ep.Name,
        Model:    req.Model,                       # 不改 model
        Reason:   "subscription quota available",
        EffectiveCost: sub.EffectiveCost,
      }
  
  # 第 2 步: 没订阅 → 按 preset (saver/balanced/quality) 走原有 cheapestProviderModel 逻辑
  return decideBySpecificPreset(req, preset)
```

**关键不变量**：

- 订阅命中时**不改 model id**（透明代理原则 — spec/04 §B）
- 三个 preset 都一视同仁地优先订阅（月费均摊后比 token 价低两个数量级，没有 saver/balanced/quality 区分的必要）

### 9.3 用尽 fallback

```
请求: provider=minimax, model="MiniMax-M2.7"
  ↓
LookupSubscription: tier="MiniMax-M*", remaining=0
  ↓ 不命中
fallback 到 cheapestProviderModel(protocol="anthropic", model="MiniMax-M2.7")
  ↓
候选 endpoint:
  - minimax static key (pay-per-token, 来自 inherited_endpoints.api_key)
  - openrouter (如果用户也配过, OpenRouter 上的 anthropic 系列)
  - anthropic 直连 (如果 inherit 有 key)
  ↓
按 token_price_api cost 排序选 cheapest
  ↓
broadcast SSE "subscription_exhausted":
  {provider: "minimax", tier: "MiniMax-M*", resets_at: <window_end>}
UI 顶部弹 toast 提示
```

---

## 10. Poll 策略

```go
// internal/providers/minimax/quota.go (已实装 v2.0.44, 微调)

func (p *QuotaPoller) Start(ctx context.Context) {
    // 启动延迟 10 秒, 避免 daemon 启动时的拥堵
    select {
    case <-time.After(10 * time.Second):
    case <-ctx.Done():
        return
    }
    
    for {
        token := getMinimaxOAuthToken()  // 见 §6
        if token == "" {
            // 没 OAuth, skip poll
            select {
            case <-time.After(30 * time.Minute):
            case <-ctx.Done():
                return
            }
            continue
        }
        
        if err := p.pollOnce(ctx, token); err != nil {
            // 401 → token 失效, 触发 Scanner 重新扫一次 OpenClaw
            if isUnauthorized(err) {
                triggerAgentRescan("openclaw")
            }
            // 其它错误 → log warn, 不重试, 等下一周期
        }
        
        // 决定下个周期 — 近重置时刻 5 min, 否则 30 min
        next := nextInterval(getCachedQuotaRows())
        select {
        case <-time.After(next):
        case <-ctx.Done():
            return
        }
    }
}

func nextInterval(rows []QuotaRow) time.Duration {
    // 找最早一个窗口的剩余时间
    soonest := math.MaxInt64
    for _, r := range rows {
        toReset := r.WindowEnd - now()
        if toReset > 0 && toReset < soonest {
            soonest = toReset
        }
    }
    if time.Duration(soonest) * time.Millisecond < 30 * time.Minute {
        return 5 * time.Minute
    }
    return 30 * time.Minute
}
```

**频率参数（可后续调优）**：
- 正常窗口：30 min
- 临近重置（< 30 min）：5 min（避免漏过 reset 时刻让 quota 计算偏差）
- 全部 quota 用尽：保持 30 min（等自然重置）

---

## 11. Effective cost derivation

The UI and the routing engine both need a per-call cost figure for a subscription tier so it can be compared against per-token vendors. The vendor's quota API does not return the monthly price, so we maintain a static mapping.

### 11.1 Pricing table (Phase 1: embedded in Go)

Data source: <https://platform.minimaxi.com/subscribe/token-plan?tab=individual__monthly>
(the 国内 / `minimaxi.com` monthly-plan page; this is the same API the OpenClaw `minimax-portal` provider authenticates against).

| total_count / 5h | highspeed | Monthly (¥CNY) | Monthly (≈ USD) | Effective / call (USD) |
| --- | --- | --- | --- | --- |
| 600   | false | 29  | $4.00   | $0.0000463 |
| 1500  | false | 49  | $6.76   | $0.0000313 |
| 4500  | false | 119 | $16.42  | $0.0000253 |
| 1500  | true  | 98  | $13.52  | $0.0000626 |
| 4500  | true  | 199 | $27.46  | $0.0000424 |
| 30000 | true  | 899 | $124.06 | $0.0000287 |
| 30000 | false | ?   | —       | — |
| 600   | true  | ?   | —       | — |

The last two rows (`{30000, false}` standard 30k, `{600, true}` highspeed 600) have not been verified against the public `minimaxi.com` page; they are treated as unknown SKUs. The lookup returns 0 for them, which routing interprets as "free" (the user has already paid for the subscription — we simply do not have the price). Update the table when the SKU list changes.

### 11.2 Formula (important!)

```
effective_cost_per_call_usd
  = monthly_price_cny × cny_to_usd
  / (total_count_per_window × windows_per_month)

windows_per_month = 144   # 30 days × 24h / 5h
cny_to_usd        = 0.138 # fixed rate; ~5% off the live FX is acceptable here
```

**`total_count` is the per-5h-window quota, not the monthly quota.** Monthly call total = `total_count × 144`. An earlier draft of this spec missed the `× windows_per_month` factor; the duplicate table added at `internal/providers/minimax/pricing.go::EffectiveCostPerCallUSD` (since deleted) followed the broken formula and reported costs about 1043× the value the routing engine was computing. PR review caught the conflict before the PR merged; only one table now exists, in `internal/storage/subscription_quota.go`.

Example: 1500 calls per 5h on the ¥49 standard plan →
`49 × 0.138 / (1500 × 144) ≈ $0.0000313 / call`, i.e. 216,000 calls/month, roughly two orders of magnitude below per-token vendors like deepseek.

### 11.3 Implementation locations (post PR #1)

Single source of truth:
- `internal/storage/subscription_quota.go::minimaxPlanPriceCNY` — the lookup table
- `SubscriptionQuota.EffectiveCostUSD()` and `MonthlyPriceUSD()` — derived helpers

Consumers:
- Routing engine → `cmd/krouter/serve.go::subscriptionSource.GetSubscriptionInfo` → `routing.SubscriptionInfo.EffectiveCostUSD`
- Dashboard API → `internal/api/subscription_status.go::tiersToJSON` → the same two helpers

Both paths share the lookup table and derived formula. **No parallel table may be introduced elsewhere in the tree** (the PR review history is the reason this rule exists).

### 11.4 Future: ETag-synced pricing data (Phase 2)

Migrate the lookup table to `data/subscription_pricing.json` in the `krouter-data` repo and reuse the 10-minute ETag-conditional sync mechanism already used for model discovery. This decouples the release cadence from pricing data. For Phase 1 the Go-embedded table is sufficient — MiniMax pricing changes roughly quarterly.

---

## 12. API（新增 / 调整）

### 12.1 GET /internal/subscription/status

返回所有 vendor 的订阅 quota 状态（给 Dashboard 用）：

```json
[
  {
    "provider": "minimax",
    "source_agent": "openclaw",
    "oauth_present": true,
    "last_polled_at": "2026-05-21T07:42:00Z",
    "last_error": null,
    "tiers": [
      {
        "tier_name": "MiniMax-M*",
        "total": 1500,
        "used": 21,
        "remaining": 1479,
        "window_start": "2026-05-21T15:00:00+08:00",
        "window_end":   "2026-05-21T20:00:00+08:00",
        "seconds_to_reset": 16113,
        "effective_cost_per_call_usd": 0.032666666,
        "matched_models_sample": ["MiniMax-M2.7", "MiniMax-M2.7-highspeed"]
      },
      ...
    ]
  }
]
```

### 12.2 POST /internal/subscription/refresh

强制立即 poll 一次（不等周期）：

```json
{ "provider": "minimax" }   // 或省略, 默认刷所有
```

返回 200 + 新数据（同 GET 结构）。

### 12.3 SSE event `subscription_exhausted`

```json
{
  "type": "subscription_exhausted",
  "data": {
    "provider": "minimax",
    "tier": "MiniMax-M*",
    "window_end": "2026-05-21T20:00:00+08:00"
  }
}
```

前端收到后弹 toast。

---

## 13. 反规范响应的解析陷阱

minimax `/v1/token_plan/remains` 在认证失败时**返回 HTTP 200**，错误信息在 body：

```http
HTTP/1.1 200 OK
Content-Type: application/json

{"base_resp": {"status_code": 1004,
               "status_msg": "login fail: Please carry the API secret key..."}}
```

**daemon 必须解析 body**：

```go
func parseQuotaResponse(body []byte) (*QuotaData, error) {
    var resp struct {
        BaseResp struct {
            StatusCode int    `json:"status_code"`
            StatusMsg  string `json:"status_msg"`
        } `json:"base_resp"`
        ModelRemains []QuotaTier `json:"model_remains"`
    }
    if err := json.Unmarshal(body, &resp); err != nil {
        return nil, err
    }
    if resp.BaseResp.StatusCode != 0 {
        return nil, fmt.Errorf("minimax error %d: %s",
            resp.BaseResp.StatusCode, resp.BaseResp.StatusMsg)
    }
    return &QuotaData{Tiers: resp.ModelRemains}, nil
}
```

**仅检查 HTTP code 是 200 是不够的** — daemon 会把"未授权"误判为 0 配额，silent 路由错。

---

## 14. Per-feature quota（未来扩展 Phase 3+）

minimax 套餐里有 feature-specific tier：

```
coding-plan-search    150 次/5h   ← 用户用 minimax web-search tool 时扣这个
coding-plan-vlm       150 次/5h   ← 用户发图像输入时扣这个
MiniMax-M*           1500 次/5h   ← 普通文本对话扣这个
```

**当前不区分 feature**：所有 minimax 请求都走 `MiniMax-M*` tier。这跟实际计费可能有少许偏差（用了 search/vision 还在算 MiniMax-M* tier），但对用户感知影响有限。

**未来扩展（Phase 3+）**：
- routing engine 解析请求的 metadata（HasImages / HasTools / 具体 tool 名）
- 按 feature 匹配到不同 tier
- 多个 tier 用尽时分别 fallback

----

## 15. 与现有代码的关系（v2.0.50 现状）

### 15.1 已实现 ✓

| 项 | 文件 | 备注 |
| --- | --- | --- |
| subscription_quota_cache 表 | `internal/storage/migrations/006_subscription_quota.sql` | schema 完整 |
| QuotaPoller | `internal/providers/minimax/quota.go` | 30min/5min 周期已实装 |
| SubscriptionSource interface + routing 集成 | `internal/routing/engine.go` + `internal/api/server.go::newSubscriptionSource` | v2.0.44 引入 |
| OAuth token 缓存（旁路）| `internal/providers/minimax/adapter.go::CacheOAuthToken` | 从 proxied 请求 header 捕获 |
| HTTP 200 + body status_code 解析 | `internal/providers/minimax/quota.go::parseQuotaResponse` | 已修 |

### 15.2 需要改造

| 项 | 现状 | 改造方向 |
| --- | --- | --- |
| OAuth token 来源 | 仅从 proxied 请求 header 捕获 | **优先从 `inherited_endpoints.extras_json` 读**（per spec/04），request header 捕获降为 fallback |
| Effective cost 来源 | minimax/quota.go 硬编码 4 档 | 改用 `data/subscription_pricing.json`（10min ETag 同步） |
| Static key 模式的 UI 提示 | 无 | 检测到 inherited_endpoints 是 `api_key`（非 OAuth）→ UI 标灰 + 文案 |
| SSE event `subscription_exhausted` | 未实装 | 加 broadcast，前端 toast |
| 401 自动触发 rescan | 未实装 | poller 拿到 401 → 调用 `agentscan.Trigger("openclaw")` |

### 15.3 待新增

| 项 | 文件 |
| --- | --- |
| `data/subscription_pricing.json` | krouter-data repo |
| `GET /internal/subscription/status` API | `internal/api/server.go` |
| `POST /internal/subscription/refresh` API | 同上 |
| Dashboard Subscription Quota 卡片 | `frontend/src/pages/Dashboard.tsx`（或独立 Subscription 页） |

---

## 16. 实施阶段

### Phase 1（紧跟 spec/04 落地，1 周内）

- [ ] OAuth token 来源切到 `inherited_endpoints.extras_json`（依赖 spec/04 Scanner 落地）
- [ ] static key 模式 UI 标灰提示
- [ ] Subscription Quota 卡片 (Dashboard)
- [ ] `GET /internal/subscription/status` API
- [ ] `POST /internal/subscription/refresh` API

### Phase 2（按 vendor 扩展）

- [ ] `data/subscription_pricing.json` 第一版（仅 minimax 4 档）
- [ ] 10min ETag 同步机制接入此文件
- [ ] SSE `subscription_exhausted` + 前端 toast
- [ ] 401 自动触发 agent rescan

### Phase 3（细化 / 未来）

- [ ] Per-feature quota（区分 coding-plan-search 等）
- [ ] 加入第二家订阅 vendor（如果有）
- [ ] effective_cost 实时显示 / 历史 trend 图

---

## 17. 已确认的设计取舍

| 取舍 | 决定 | 理由 |
| --- | --- | --- |
| OAuth token 是否长期持久化在 krouter | ❌ 不持久化 | 用户凭证敏感，仅运行时 inherit，过期由 user agent 自己刷 |
| 是否让用户手动填 vendor 月费 | ❌ 不让 | 用户不一定记得档位；研发人工维护 JSON 表 |
| Per-feature quota 是否 Phase 1 做 | ❌ 不做 | 影响有限，Phase 3 按需 |
| 三个 preset 是否区分订阅优先级 | ❌ 不区分 | 月费均摊后比 token 价低两个数量级，所有 preset 都该优先订阅 |
| 订阅命中时是否改写 model id | ❌ 不改 | 透明代理原则，model 字段保持不动 |
| 静态 API key 模式是否走"订阅"机制 | ❌ 不走 | 没 quota 数据，silent skip + UI 标灰 |
| 是否新增表存 token | ❌ 不新增 | `inherited_endpoints.extras_json` 已足够 |
| HTTP 200 + body status_code 这种反规范设计的应对 | ✅ 必须解析 body | 一个 vendor 这样做未来可能更多，daemon 解析层要鲁棒 |
| user budget downgrade 是否合并到本机制 | ❌ 不合并 | 两套机制解决不同问题（vendor 上限 vs 用户预算），并存 |
| 加新订阅类 vendor 是否需要发版 | ✅ 需要 | Poller 是 Go 代码（解析逻辑各家不同）；但 effective_cost 表纯 JSON 可热更 |

---

## 18. 产品话术（marketing 参考）

```
其它 LLM 路由的 quota 体验:
  你的 MiniMax 套餐用完不知道 → 继续按订阅 baseUrl 发请求 → 直接失败
  或: 自己定期登 minimax 网站看额度

krouter 的体验:
  Dashboard 实时显示套餐余量 (1479/1500, 4h28m 后重置)
  套餐充足时所有请求免费用订阅
  用尽时自动切到 pay-per-token 候选, UI toast 提醒
  重置时刻自动恢复订阅优先

差异化卖点:
  "你买的套餐, krouter 自动榨干每一份额度, 用完才花新钱."
```
