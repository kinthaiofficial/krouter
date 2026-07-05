# Changelog

All notable changes to krouter will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [2.6.1] - 2026-07-05

### Fixed
- **Streaming usage from MiniMax-style Anthropic-compatible endpoints is no longer lost**: the SSE usage parser read input/cache tokens only from `message_start` (the official Anthropic shape), but MiniMax sends placeholder zeros there and the real cumulative usage — including `input_tokens` and `cache_read_input_tokens` — in the final `message_delta`. Every streaming MiniMax request was therefore recorded as 0 input / $0, understating cost, budget and savings. Usage from `message_start` and `message_delta` is now merged per message with non-zero delta fields overriding; since `message_delta` usage is cumulative per the Anthropic spec, this also stops periodic-delta implementations from being double-counted.
- **Cost lookup now finds provider-prefixed catalog entries**: agents send bare model ids (`MiniMax-M3`) but LiteLLM catalogs non-flagship vendors only under `<provider>/<model>` (`minimax/MiniMax-M3`), so the exact-match lookup priced such requests at $0 even when tokens were counted. `CostFor` now falls back to the provider-qualified key, translating krouter adapter names to LiteLLM's where they differ (fireworks → fireworks_ai).
- **`krouter logs` shows raw token counts below 1K** instead of rounding a 37-token request down to "0K", which had masked the zero-usage bug above.

## [2.6.0] - 2026-07-05

### Changed
- **`krouter logs` now reads like `tail`**: entries print oldest→newest so the latest request sits at the bottom (it used to be first), and the new `-f`/`--follow` flag streams requests live as they complete — snapshot first, then the daemon's SSE feed, subscribed before the snapshot fetch so nothing is missed and deduplicated by id so nothing prints twice. `-n`/`--lines` (default 50) is unchanged.

### Fixed
- **OpenClaw takeover no longer crash-loops the gateway on OpenClaw ≥2026.6.9**: connect used to stash each provider's original endpoint in a `_krouterOriginalBaseUrl` field inside `openclaw.json` (and sub-agent `models.json`), relying on OpenClaw ignoring unknown fields. Since 2026.6.9 `models.providers.*` is strictly validated and any unknown field fails the whole config ("Invalid input"), so the gateway refused to start after takeover. Originals are now stored in krouter's own `~/.kinthai/openclaw-restore.json`; disconnect restores from it (legacy in-file sidecars are still honored, and reconnecting — e.g. after upgrading krouter — strips them from the config, self-healing already-broken installs). An endpoint the user has meanwhile re-pointed by hand is never clobbered on disconnect.
- **`krouter install` (CLI / headless) now actually registers connected agents with the daemon**: only the GUI wizard wrote `pending-agents.json`, so a CLI install left `app_settings` empty — the inheritance scan never ran, no endpoints were known, and every request through the rewritten agent configs was misrouted (e.g. MiniMax models sent to Anthropic → 502, DeepSeek to OpenAI → 401). The CLI now writes the same pending file for each successfully connected agent, connects agents *before* starting the service so a fresh install routes correctly from the first request, and the daemon additionally consumes pending selections on its 1-minute rescan tick so registration never waits for a restart.

## [2.5.0] - 2026-06-11

### Added
- **Editable config paths are now discoverable in the Apps page**: each app's scanned config path (e.g. `~/.pi/agent/models.json`) is a clickable control with an always-visible "Edit" label and a hover affordance, instead of a faint 12px pencil icon that was easy to miss. Users whose agent isn't installed at the default location can click the path, correct it, and hit "Save & rescan" — no hunting required.

### Changed
- **Providers page only shows a protocol badge once a provider is configured**: the protocol badge (and the Protocol detail row) used to appear on every card, including unconfigured ones — wrongly implying a provider is pinned to that single protocol. krouter routes by whatever protocol you actually configure and refuses none, so unconfigured providers no longer assert a protocol; configured providers still show the protocol they're actually in use with.

### Changed
- **Inherited credentials never touch disk anymore**: API keys and OAuth tokens that the agent scanner reads from your AI app configs (OpenClaw, Codex, …) are no longer written to SQLite. They now live only in the daemon's memory, repopulated from your agent's own config files at startup, on the 1-minute periodic rescan, and on manual rescans — so rotating a key in your agent config is picked up within a minute and krouter never holds a stale copy. Migration 022 drops the `inherited_endpoints.api_key` column, scrubs `oauth_token` values out of `extras_json`, and VACUUMs the database so the old secret bytes are physically erased from the file, not just logically deleted. You can verify with any SQLite browser: `~/.kinthai/data.db` contains no credentials, period.

### Fixed
- **Free models now win the Saver cheapest-model scan**: the routing engine's pricing lookup used a bare `float64` where `0` meant both "unknown model" and "free model", so a genuinely $0-priced model (e.g. `:free` catalog entries) could never be selected — contradicting the documented "a $0 price falls out as cheapest automatically" behavior. `PricingSource.InputCostPerToken` now returns `(cost, ok)`; free models rank normally and unknown ones stay unranked.
- **Two cross-protocol model-name leaks**: (1) Saver's multimodal branch hardcoded `claude-sonnet-4-5` for *any* protocol, sending an Anthropic model id to OpenAI-protocol providers on image requests; it now honors the requested model outside the Anthropic protocol. (2) Quality's Opus-24h-cap downgrade rewrote even OpenAI-protocol requests to `claude-sonnet-4-6`; the downgrade is now Anthropic-only.
- **Circuit breaker now sees mid-fallback failures**: a provider that returned 5xx (or a network error) and was abandoned for a fallback never had the failure recorded — only the final attempt's outcome was logged — so a consistently broken provider with a working fallback never tripped the 3-failure breaker and every request probed it first. Abandoned attempts now record their failure.
- **`GET /v1/models` no longer forwards third-party keys to Anthropic**: the endpoint unconditionally proxied to `api.anthropic.com` with all request headers, including API keys belonging to OpenAI-protocol providers. Requests without Anthropic-style credentials (x-api-key, or a `sk-ant-` Bearer token) are now answered locally from the registry's OpenAI-protocol model list; nothing is forwarded.
- **Session stickiness no longer breaks on provider registration order**: the bound-provider liveness check resolved the provider via "first registry entry supporting the model", so a session bound to a later-registered provider was wrongly treated as unbound and lost its prompt cache. The provider is now looked up by name.
- **Failed responses no longer pollute session state**: non-streaming 4xx/5xx responses used to bind the session's sticky target and bump its counters; only 2xx responses update sessions now.
- **Image payloads no longer inflate the token estimate**: `InputTokenEst` counted base64 image bytes as text (~330k "tokens" per MB), misclassifying every image request as complex and distorting cost estimates. Image block payload size is now excluded.
- **Anthropic-tier fallback checks for a configured key** before offering a lower tier, consistent with the rest of selection (#47).
- **Codex scanner no longer resolves `env_key` through the daemon environment**: krouter does not read environment variables for credentials (privacy); the lookup also could not see user-shell exports, so it only ever misfired. The request's own key is forwarded as-is instead.
- **Hung upstreams now fail fast**: outbound transports set a 60s `ResponseHeaderTimeout` (streaming bodies remain unbounded), so a provider that accepts the connection but never responds triggers fallback instead of holding the request open indefinitely.
- **Inherited keys now resolve across vendor name aliases**: a provider you configured in your agent under its natural vendor name (e.g. `dashscope` in OpenClaw) is now matched to krouter's adapter name (`qwen`) when resolving the inherited API key. Previously the lookup was an exact string match, so such a provider showed `configured: false` on the Providers page and couldn't be routed to — even though the credential was present — unless you renamed it in your agent config to match krouter's name. Known aliases (`dashscope`→`qwen`, `together_ai`→`together`, `fireworks_ai`→`fireworks`) are now collapsed at lookup time; the original name is still stored as scanned.
- **Cold-start `500 database error` on the management API**: the SQLite DSN used mattn/go-sqlite3-style parameters (`_journal_mode`, `_busy_timeout`) that the actual driver (`modernc.org/sqlite`) silently ignores, so the database ran with `journal_mode=delete` (writers block readers) and `busy_timeout=0` (no wait on contention) instead of the intended WAL + 5s timeout. During the first few seconds after startup — while the embedded pricing seed (2745 models) is written — concurrent reads/writes hit `SQLITE_BUSY`, so endpoints like `/internal/logs`, `/internal/models`, and `/internal/apps/configured` briefly returned `500`. The DSN now uses modernc's `_pragma=` syntax so WAL and the busy timeout actually take effect; a cold-start stress test that previously produced 52 `500`s now produces zero.

### Changed (internal)
- Routing decisions read quota state once per request and the daemon-side quota source caches it for 3s, removing several per-request SQL aggregations from the hot path. Request-record persistence moved from one goroutine per request to a single writer goroutine fed by a bounded queue, serialising SQLite writes. Agent scanners and the upgrade-apply goroutine are now panic-isolated. The MiniMax in-memory OAuth token cache expires after 30 minutes. App-config connect diffs mask key/token/secret values before leaving the daemon.

### For contributors
- Integration auth tests were updated to the Origin-based CSRF model (foreign Origin → 403, valid token bypasses the guard), replacing the obsolete token-required assertions. The wine/linux integration suites now live under `tests/` in the repo (previously workspace-local); `tests/wine/run_tests.sh` was brought up to date with the current auth model, UI path, and preset endpoint, and two `set -e` bugs that aborted the suite early were fixed.

## [2.4.10] - 2026-05-27

### Changed
- **Pricing data pipeline: LiteLLM → krouter CDN → daemon**: the daemon no longer fetches directly from LiteLLM's raw GitHub JSON. A new daily GitHub Action (`update-litellm-prices.yml`) fetches from LiteLLM, merges with the local supplement file (`data/token_prices_ext.json`), and commits `data/token_prices.json` to the repo in a stable krouter-owned format. The daemon fetches from `krouter.kinthai.ai/data/token_prices.json` (primary) with `raw.githubusercontent.com` as fallback — the same CDN + GitHub Raw pattern used by subscription pricing. This isolates all krouter daemons from LiteLLM format changes: a format break now only affects the daily Action and is fixed with one commit. The file is also embedded via `go:embed` so freshly-installed daemons have full pricing data offline without waiting for a network sync.

### Added
- **`data/token_prices_ext.json`**: manually-maintained supplement for models absent from LiteLLM. Initial entries: `claude-3-5-sonnet-20241022`, `claude-3-5-haiku-20241022`, `deepseek-coder`, `MiniMax-M2.7`, `MiniMax-M2.7-highspeed`. Prices sourced from respective vendor pricing pages.

## [2.4.8] - 2026-05-27

### Fixed
- **Anthropic streaming token counts always 0 for opus-4 and newer models** (#72): when a client (e.g. OpenClaw) sends `Accept-Encoding: gzip`, krouter was forwarding it to Anthropic, which then returned a gzip-compressed SSE stream. Because Go's Transport only auto-decompresses when it added the `Accept-Encoding` header itself, the SSE capture buffer received raw compressed bytes — causing `parseAnthropicSSEUsage` to return 0 for all token buckets. The Anthropic and OpenAI adapters now strip `Accept-Encoding` before forwarding, so Go's Transport manages compression negotiation and transparently decompresses both streaming and non-streaming responses.

## [2.4.7] - 2026-05-27

### Fixed
- **Quality preset no longer over-upgrades simple requests to opus** (#65): `ComplexityScore` previously treated `HasTools=true` and system-prompt keywords (e.g. "debug", "refactor") as complexity signals, which caused every Claude Code / Cursor request to score as "complex" and get upgraded to opus — bypassing the MiniMax subscription benefit. Those signals are removed; complexity now depends only on `HasImages` and `InputTokenEst` (>10 k tokens → complex, 4 k–10 k → medium).
- **Apply Update button no longer stays stuck on "Restarting…"** (#71): after the daemon restarts from a self-update, the About page now polls `/internal/status` every 1.5 s until the version changes (or a 2-minute timeout), then resets the mutation state and re-runs the update check so the UI reflects the new version instead of staying frozen.

## [2.4.6] - 2026-05-26

### Changed
- **Renamed "agent" → "app" throughout**: the word "agent" was ambiguous — it referred to both the AI applications (openclaw / claude-code / cursor / codex) that are krouter's direct clients, and to named configuration profiles *inside* those applications. The canonical term is now "app" at every layer: DB columns (`agent_settings` → `app_settings`, `agent_id` → `app_id`, `requests.agent` → `requests.app`), Go types and function names, HTTP routes (`/internal/agents/*` → `/internal/apps/*`), SSE / JSON field names, the web dashboard ("Agents" page → "Apps" page), and i18n keys (`agents.*` → `apps.*`). The `SubAgentSection` component is removed — individual agent profiles inside an app are not observable from HTTP traffic and are not shown in the UI.

### Added
- **Key-hint routing channels on the Apps page**: the `requests` table gains a `key_hint` column (last 4 characters of the api_key extracted from `x-api-key` or `Authorization: Bearer` headers). The Apps page now shows the distinct routing channels used by each app — each channel corresponds to one api_key suffix, its request count, and its cost. `NULL` for pre-migration rows; `""` for requests with no api_key. Migrations 018 and 019.

## [2.4.5] - 2026-05-26

### Changed
- **DecisionCard header redesigned**: savings pill, routing summary line, and app chip moved into the card header with a shadow separator between the latest card and the history list.

## [2.4.4] - 2026-05-26

### Added
- **Passthrough preset**: new fourth routing mode (`passthrough`) that forwards the exact model the client requested without any substitution. The routing engine resolves which provider serves the model and passes through unchanged; quota downgrade and cache-sticky routing both skip passthrough. Migration 017 adds a `routing_preset` column to the `requests` table; all four `logRequest` call sites record the active preset. The Dashboard KPI strip relabels "requests" as "routes" and adds a per-preset breakdown row (saver / balanced / quality / passthrough) showing request count, savings USD, and savings percentage.

### Removed
- **Dashboard "Recent Requests" section**: the Router page already surfaces this data in a better format; the duplicated table on the Dashboard was removed.

## [2.4.3] - 2026-05-26

### Changed
- **Router page redesigned**: routing history is now a scrollable list of inline cards with infinite scroll (replaces the fixed-height table). A "Logs" button in the header links to the full searchable archive. Token breakdown split into 4 labelled rows (input / output / cache-read / cache-write).

## [2.4.2] - 2026-05-26

### Added
- **Cache read/write pricing in routing decision card**: `cache_read_per_mtok` and `cache_write_per_mtok` appear in the Request section of each decision card. Rows are hidden for models without prompt-caching support (e.g. DeepSeek, MiniMax), so non-Anthropic cards are unaffected.
- **Per-request cache hit rate in response card**: observed hit rate (`cache_read / total`) is displayed alongside the token breakdown in the expanded card.
- **Gzip-compressed upstream response support**: krouter now handles `Content-Encoding: gzip` responses by forwarding the original compressed bytes to the client unchanged while decompressing a separate in-memory copy for token parsing.

### Fixed
- **Cache hit rate math corrected**: `ObservedHitRate` (this turn: `cache_read / total`) and `PredictedHitRate` (next-turn estimate: `(cache_read + cache_write) / total`) are now distinct. Sticky routing uses the predicted rate; the decision reason string shows the observed rate. `OutputShare` denominator now correctly includes `CacheWriteTokens`.
- **Transparent gzip passthrough**: the `decodeResponseBody` helper that was stripping `Content-Encoding` before forwarding is replaced by `decompressForParsing`, which reads a separate in-memory copy for token counting while leaving the original bytes and headers intact for the client.

### Removed
- **`X-Krouter-Provider` and `X-Krouter-Model` response headers**: these headers were injected into every forwarded response, modifying transparent passthrough silently. Routing information is available via `/internal/logs` instead.

## [2.4.1] - 2026-05-26

### Added
- **Cache-write tokens surfaced in all UI pages**: `cache_write_tokens` is now tracked end-to-end from database to the browser. Logs CSV export gains a `cache_write_tokens` column; the Providers page shows a "Lifetime cache-write" row when non-zero; the Agents page token-total column now sums all four token buckets (fresh input + output + cached + cache-write); the RoutingDecision card appends "N cache-write" to the token breakdown when cache writes occurred. SSE `request_completed` events carry the field so live views update without a page refresh. Translations added for both English and Chinese.

## [2.4.0] - 2026-05-26

### Added
- **Cache-aware sticky routing** (Phase 3): the routing engine now tracks per-session token-bucket stats (fresh input / cache-read / cache-write / output) and short-circuits to the session's bound (provider, model) when the observed cache hit rate exceeds the mathematical breakeven threshold `p* = (1 − 1.25 × P_candidate / P_bound) / 0.9`. Switching model mid-session requires paying a 25% write surcharge on a cold cache; sticky routing fires only when staying put is mathematically cheaper. Active for Saver and Balanced presets; Quality preset is unchanged. Eight decision rules cover output-share guard (>30% falls through), bound-provider liveness, cheapest-alternative scan, and breakeven comparison.
- **Session tracking infrastructure** (Phase 2): `MemSessionStore` (in-memory, 75-minute TTL, background eviction) accumulates cache hit rates across turns in the same agent conversation. Session key is a 16-char SHA-256 fingerprint of `(api_key + system_prompt + tool_names + first_user_message)` — stable across turns, distinct across agents and users. Data feeds Phase 3 routing decisions without being persisted to disk.
- **Session-aware savings annotation**: `enrichDecision` now computes the savings percentage using the actual session cache hit rate (`keptCost = price × tokens × (hitRate×0.1 + (1−hitRate))`) instead of a bare per-token comparison that ignored cache state. Falls back to the conservative cache-cold estimate on the first turn of a new session.

### Fixed
- **Cache write cost was under-charged by 25%**: `pricing.CostFor` applied the standard input rate to `cache_creation_input_tokens`; Anthropic charges a 1.25× surcharge on cache writes. Fixed with a new `cacheWriteTokens` parameter; all call sites updated.
- **OpenAI `cached_tokens` were never parsed**: the OpenAI and DeepSeek response parsers read `prompt_tokens` as the full input count without subtracting `cached_tokens` / `prompt_cache_hit_tokens`, so cache-read tokens were billed at the full input rate instead of 10%. Both JSON and SSE parsers now extract the two cached-token fields and adjust `InputTokens` to fresh-only.
- **`cached_tokens` were always 0 in the database**: none of the four `logRequest` call sites populated `CachedTokens` from the parse result. All four sites now carry both `CachedTokens` and `CacheWriteTokens` into the DB row.
- **Savings percentage was computed from wrong baseline**: `enrichDecision` compared the routed model's cost against the *requested* model's cost without accounting for cache state, producing numbers that could be badly wrong for long sessions with high hit rates. The display was removed in Phase 1 and restored in Phase 3 with the session-aware formula.

### Changed
- `storage.RequestRecord` gains a `CacheWriteTokens int` field; migration `016` adds the `cache_write_tokens` column (nullable, default 0 — safe on existing databases).
- `pricing.CostFor` and `pricing.BaselineCostFor` signatures gain `cacheWriteTokens int` as the last parameter. All internal call sites updated; external callers (none outside this repo) must add the parameter.

## [2.3.14] - 2026-05-26

### Fixed
- **MiniMax requests no longer 401 when re-routed via the subscription shortcut** (#63): the MiniMax adapter forwarded the inbound auth header as-is, so a request the engine re-routed here carrying another provider's credential (e.g. an OpenClaw `claude` sub-agent sending an Anthropic `x-api-key`) reached MiniMax without its OAuth Bearer and was rejected. The adapter now injects `Authorization: Bearer <token>` (resolved from `inherited_endpoints.extras_json` — scanned from `auth-profiles.json` — falling back to the in-memory request-header cache) and drops the stale `x-api-key`. When no token is resolvable it falls back to transparent passthrough, so a direct `minimax-portal` request is unaffected.
- **Budget-blocked requests are written to the dashboards** (#66): the `ErrBudgetExceeded` path returned `429` without logging, so budget-blocked requests vanished from Router/Logs and per-provider stats (the #52 fix only covered the fallback-exhausted `502` path). Both handlers now write a durable `429` log row before responding.
- **No more negative "savings" for unpriced models** (#64): after #53 (a `$0` baseline for models absent from the pricing catalog), the per-request savings banner rendered `baseline − cost` as a negative number. The daemon now omits the baseline from the live event when it is `0` (matching `/internal/logs`), and the UI renders the savings banner only when the baseline is positive — an unpriced model shows no banner instead of a misleading loss.

## [2.3.13] - 2026-05-26

### Changed
- **Application attribution is now decided at connect time, not guessed from headers** (#49): connecting an app bakes its id into every base URL krouter writes — the origin (`scheme://host:port`) is replaced with `http://127.0.0.1:8402/a/<appid>` while the original path is preserved verbatim (so `/v4`, `/anthropic/v1`, … survive; no protocol guessing, no `/v1` insertion). The proxy strips the `/a/<appid>` prefix, attributes the request to that application deterministically, and dispatches to the protocol handler by path suffix (`/messages`, `/chat/completions`, `/models`). Requests without the prefix fall back to the legacy `User-Agent`/header sniff; an unknown app id or unsupported suffix returns 404. OpenClaw's OpenAI-protocol traffic (DeepSeek / Z.AI / …) is now logged under `openclaw` instead of `unknown`. Applies to all six connectable apps (OpenClaw global + per-agent providers, Claude Code, Cursor, Codex, OpenCode, Hermes), with the user's original endpoint saved for exact restore on disconnect. Internal: `routing.Request.AgentName` renamed to `AppID` (the `requests.agent` column is unchanged).

## [2.3.12] - 2026-05-26

### Added
- **OpenClaw takeover now covers every configured provider, not just Anthropic** (#59): connecting OpenClaw used to redirect only the global `anthropic` provider (plus `minimax-portal`) to krouter, so its OpenAI-protocol providers (DeepSeek, Z.AI, OpenAI, …) kept talking to the upstream directly and could never be routed or saved on. Connect now rewrites every provider in the global `models.providers` **and** in each sub-agent's `agents/<id>/agent/models.json`, using the bare proxy base for anthropic-family providers and the `/v1` base for openai-family ones. The original endpoint is stashed in a krouter-managed `_krouterOriginalBaseUrl` sidecar so disconnect restores it exactly; `apiKey` is never touched.

### Removed
- **Dropped the redundant `model_catalog` table** (#60, migration `015`): it was written by the pricing sync but never read for routing — model counts and prices come from `token_price_api`, and routable availability now comes from `/v1/models` discovery.

### Fixed
- **Providers recover after transient failures instead of being excluded forever** (#48): `isHealthy` treated `consecutive_failures >= 3` as permanently unhealthy, and `cheapestProviderModel` / `mostExpensiveProviderModel` skip unhealthy providers — so three transient 4xx/5xx (e.g. during a key-refresh window) demoted a provider with no way back, since nothing routed there to clear the count. A provider now gets a half-open probe once `healthRecoveryTTL` (5 min) has elapsed since its last failure; a success clears the count, a failure re-arms the window.
- **Savings numbers are no longer fabricated for unknown models** (#53): `BaselineCostFor` used to substitute claude-sonnet-4-5's price when the requested model wasn't in the catalog, inventing a "savings" figure against a price the user never faced — and contradicting `PriceFor`, which returns 0 for the same model. It now returns 0 for unknown models, so the Router/Logs/budget savings only reflect a real baseline-vs-actual comparison.
- **Failed requests are no longer silently dropped from the dashboards** (#52): when the fallback chain was exhausted (e.g. all upstreams unreachable), the proxy returned `502` without recording anything — the request vanished from Router/Logs and per-provider stats. Both the Anthropic and OpenAI handlers now write a durable log row (status 502, last attempted provider/model, latency, error message) before responding. `logRequest` skips a `provider_status` failure write when no provider was attempted.
- **Routing is deterministic and never selects keyless providers** (#46, #47, #51): `Registry.All()` / `ForProtocol()` now iterate in registration order instead of randomized Go map order, so two identical requests route reproducibly (previously the same request could land on different providers ~30s apart). And `pickHealthyProvider`, `pickProviderForModel`, and `fallbackOpenAI` now skip providers without a configured key — consistent with `cheapestProviderModel` / `mostExpensiveProviderModel` — so routing no longer wastes a request on a guaranteed 401, and the fallback chain stops churning through keyless providers (fireworks → gemini → xai → …) one 4xx at a time.

### Changed
- **Routing model availability now comes from live `/v1/models` discovery instead of the LiteLLM catalog** (#60): the engine decides which models a provider can serve from its discovered model list — the accurate, per-key signal — and the LiteLLM sync now supplies per-token *pricing only*. The proxy lazily discovers a provider's models using the API key from the live request (async, deduplicated, 24h stale-guarded), so agents whose key krouter cannot read from config — Cursor (OS keychain) and Claude Code (shell env) — are covered as soon as traffic flows. Until a provider has been discovered its model list is empty and routing passes the request through unchanged.
- **Agent-config rescan polls every minute and skips unchanged files** (#61): the periodic rescan re-parsed every config and rewrote `inherited_endpoints` on every tick. It now stats each scanner's input files and skips the parse / DB write / SSE broadcast when nothing changed since the last scan (reusing `last_scanned_at`, no new column), so the cadence tightened from 5 minutes to 1 for faster pickup of edits while staying near-free when idle. OpenClaw's watch set includes each sub-agent's `models.json` / `auth-profiles.json`, so profile and OAuth-token edits are still detected even when the main config is untouched.
- **Post-connect restart notice is backend-driven** (#62): the connect response now carries `needs_restart` + `restart_kind` (`shell` for env-based agents like Claude Code, `process` otherwise), and the dashboard renders the hint from that instead of hard-coding it per agent id — so every agent, including Hermes, gets the correct notice. krouter still never restarts the app itself; the hint clears on disconnect.
- **Settings & Notifications pages aligned with the dashboard style; top-bar settings icon is now a gear**: the top-right settings icon switched from a sliders glyph (`Settings2`) to the conventional gear (`Settings`). The Settings page's sections (Language, Data management) now use the same titled-`Panel` style as the dashboard panels, and both Settings and Notifications widen to `max-w-6xl` so their left edge lines up with every other page. Notification cards adopt the shared card tokens.

## [2.3.11] - 2026-05-25

### Changed
- **Dashboard logo unified to the kinthai.ai brand mark**: the top-bar logo and browser favicon now use kinthai.ai's logo instead of the krouter-specific glyph, bringing the dashboard in line with the parent brand (and matching the krouter website).

## [2.3.10] - 2026-05-25

### Changed
- **Page widths unified for a consistent left edge**: the Free, Providers, and Budget pages widened to `max-w-6xl`, matching Dashboard / Logs / Router / Agents, so content fills the canvas and the left margin no longer jumps when navigating between pages. About also moved to `max-w-6xl` but lays its (sparse) Version / Resources cards in a two-column grid with the update banner full-width below it, so it aligns with the other pages without leaving the right half blank.

## [2.3.9] - 2026-05-25

### Changed
- **Agents page reframed around the AI agent rather than the host app**: the page used to show one card per app (OpenClaw, Claude Code, Cursor, …). Operator framing surfaced the issue — it's the AI agent that calls providers and burns tokens; the host app is configuration metadata for that agent. The page now leads with a "Connected agents" section showing every agent currently routing through krouter, and pushes detected-but-disconnected hosts into a compact "Available" section below. For multi-profile hosts (OpenClaw), each sub-agent profile (`claude`, `deepseek`, `main`, …) is rendered as a prominent stacked panel inside the card rather than buried behind a collapsed toggle. The page container widened from `max-w-3xl` to `max-w-6xl` (Router-page parity) so the left margin no longer wastes screen real estate on dense pages. Every provider name reference (single-profile inherited list + per-sub-agent provider rows) is now a click-to-deep-link chip pointing at `/providers#provider-<name>`.
- **Providers page supports `#provider-<name>` deep-links**: each `ProviderCard` carries an `id="provider-<name>"` anchor; navigating with that fragment auto-expands the matching card and scrolls it into view (`scroll-mt-20` keeps it clear of the sticky header). Enables one-click jumps from the Agents page (and anywhere else we want to refer to a specific provider) without forcing the user to scroll the active/inactive list.
- **Top-nav version chip removed**: the `vX.Y.Z` text next to the brand was rarely scanned and crowded the LIVE indicator on narrow viewports. Version information remains on the About page and in `/internal/status`.

## [2.3.8] - 2026-05-25

### Added
- **Free-providers catalog is now bilingual (English default, Chinese overlay)**: the curated `data/free_tokens.json` content (provider summaries, conditions, setup hints, notes, and a few CJK display names) shipped Chinese-only, so the English-default dashboard still showed Chinese on the Free page. The base string fields are now English; each provider carries an optional `i18n` object shaped `{ "zh": { "free_summary": …, "conditions": …, … } }` with the translations, and the Free page overlays the user's current UI language, falling back to the English base whenever a field/language is absent (alternate-protocol setup hints are localized the same way). Carried through storage by a new additive `i18n_json` column (migration `014`); the sync schema stays version 1 so older daemons ignore the unknown `i18n` key and keep rendering English. No routing impact — this is catalog/discovery content only.
- **Agents page surfaces OpenClaw sub-agent profiles**: OpenClaw hosts multiple named profiles (`main`, `claude`, `deepseek`, …) each with their own primary model and per-profile provider configuration in `~/.openclaw/agents/<id>/agent/models.json`. The dashboard's Agents card now lists every sub-agent under OpenClaw — display name, primary model id, OAuth presence chip, and a click-to-expand provider list (provider name, protocol, base URL, model id list, "this provider's primary model" highlight, key-configured chip). New read-only endpoint `GET /internal/agents/{name}/sub-agents` returns the breakdown; secrets stay on the daemon (the response carries `has_api_key` booleans, never the raw keys). The existing flat `inherited_endpoints` and routing engine are untouched — this is a UI-only surface.

### Fixed
- **Routing fallback now respects the request protocol**: when an OpenAI-protocol request named an unknown model (e.g. `baidu/cobuddy:free`), Balanced and Quality presets fell back to a hard-coded `claude-haiku-4-5-20251001` — an Anthropic-only model name — and sent it to an OpenAI provider (mistral / groq / etc.). The upstream rejected with HTTP 401 / 400 because the model id wasn't theirs to serve. Fix: new `fallbackModelFor(protocol)` returns `saverOpenAIModel` (`deepseek-chat`) for OpenAI and `saverAnthropicModel` (`claude-haiku-4-5-20251001`) for Anthropic. The two call sites in `decideBalanced` and `decideQuality` use it. Adds 2 regression tests using a multi-provider registry that exercise the openai unknown-model path end-to-end.

### Changed
- **Routing no longer uses the curated `data/free_tokens.json` catalog as a decision filter**: the spec/06 "free-credit-first" shortcut intersected `inherited_endpoints` with the catalog and preferred the result over the paid cheapest path. The problem the user surfaced: the catalog can never enumerate every free provider in the world, so any user-configured free provider not on the list got silently routed at "paid", and inherited entries that happened to be expensive (e.g. Anthropic) won over actually-cheaper alternatives just because they were catalogued. The right "is this free?" signal is the per-token price in `token_price_api` — a model priced at $0 falls out as cheapest automatically without a special-case path. Removed: the `routing.FreeProviderSource` interface, `engine.WithFreeProviders`, `cheapestFreeProviderModel`, `cheapestPaidProviderModel`, and the `cmd/krouter/freeProviderSource` implementation that joined catalog ∩ inherited. The catalog data layer (`internal/storage/free_providers.go`, `internal/freeproviders` sync, `/internal/free-providers` API, the dashboard's "Free tokens" page) is **kept** — it's discovery / signup UI, not a routing input.

## [2.3.7] - 2026-05-25

### Changed
- **Web UI redesigned to a high-contrast "pro control panel" theme**: the dashboard moved away from the generic admin-template look. Secondary text was the worst offender — `gray-400`/`gray-500` were hard to read — so the entire neutral ramp is remapped to a cooler, higher-contrast scale and the brand color is now emerald (`#0fa46a`). The Dashboard leads with a KPI strip (today's requests / saved / spent / saved-%, each with an accent rail), a LIVE indicator in the top bar, a provider-distribution panel with inline bars, and a recent-requests table with a status column. Shared primitives (`PageHeader`, `Panel`, `Badge`, `StatusDot`) give every page — Router, Logs, Budget, Providers, Agents, Settings, Free, About, Announcements — a consistent bordered-card layout. Figures render in the system monospace stack (`font-mono tabular-nums`); no webfonts are bundled, so the UI stays offline-capable and renders CJK through the system sans.

## [2.3.6] - 2026-05-24

### Added
- **About page now triggers a fresh update check on open**: previously the page rendered cached state from the daemon's 24 h ticker, so a user who opened it 10 hours after a new release saw "no update available" until the next tick. Now the page fires a synchronous check the moment it mounts and shows one of four states: a spinning loader with "Checking for updates…", a green check with "You're on the latest version" (with a small "Check again" link), the existing yellow "Apply Update" banner when something newer is on the manifest, or a red error block with retry on network/signature failure. New endpoint `POST /internal/update-check` calls `upgrade.Service.CheckNow(ctx)` synchronously and returns the same JSON shape as `/internal/update-status`.

## [2.3.5] - 2026-05-24

### Changed
- **Agents page: unified card per agent** — the separate "Agent Inheritance" overview section and per-agent detail cards have been merged. Each agent now appears as a single card that combines scanner metadata (config path, inherited providers, last scan time), proxy connection state (Connected / Not Connected badge), enable/disable toggle, and the full stats + logs panel. Eliminates the duplicate listing where the same agent appeared twice on the page.
- **Top nav streamlined** — nav items are now text-only (icons removed). Settings and Notifications move to the top-right icon cluster: a gear icon (⚙) links to Settings, a bell icon (🔔) links to Announcements. The bell shows a red dot when there are unread announcements. "Free tokens" nav label shortened to "Free".

## [2.3.4] - 2026-05-24

### Fixed
- **FreeTokens page: hardcoded Chinese/English strings replaced with i18n keys**: the `ProviderRow` component was rendering region labels ("国内" / "INT'L"), free-type badges ("试用赠送" / "永久免费" / "免费层"), configured/exhausted badges, validity/conditions meta, dual-protocol hint, apply/visit-site CTA, and last-verified footer — all hardcoded. All 14 label groups are now driven by `en.json` / `zh.json` keys under `freeTokens.*`. The English-default UI is now fully correct; Chinese appears only when the user switches to 中文.
- **Dashboard "Quota" section header was hardcoded English**: now uses `t('dashboard.quota')`.
- **About.tsx update description was hardcoded English**: "Version X is ready to install." now uses `t('about.update_ready', { version })`.
- **Protocol-constraint explanation in FreeTokens was Chinese-only**: `freeTokens.howto_line2` is now written in English in `en.json` and Chinese in `zh.json`, with proper i18n structure in both locales.
- **`💡` emoji in FreeTokens dual-protocol hint replaced with Lucide `Info` icon**: emoji-as-icon is an anti-pattern in dashboard UIs (no scale control, OS-dependent rendering). The indigo hint panel now uses `<Info className="w-3 h-3 text-indigo-500" />`.

### Changed
- **Language toggle added to the main header**: a small `EN / 中` button appears in the top-right of the sticky navigation bar. Clicking it calls `i18n.changeLanguage()` and writes to `localStorage` (key `krouter:lang`) for persistence across reloads. The toggle shows the *other* language as the action label ("中" when the UI is in English, "EN" when in Chinese).
- **Contrast improvements across all pages (WCAG AA)**: functional text that previously used `text-gray-400` (contrast 2.96:1, below the 4.5:1 AA threshold) has been upgraded to `text-gray-500` (contrast 4.60:1, passes AA). Affected: page subtitles, stat labels, table column headers, timeline timestamps, section-divider text, install-wizard notes. Decorative elements (icons, transient loading pulses, truly de-emphasised footnotes) remain at `gray-400`.
- **Dashboard visual hierarchy**: the "Today" stat card receives a 3 px brand-green left border to distinguish it as the live-state card vs the "This Week" summary card. All section headers normalised to `text-xs font-semibold uppercase tracking-wider text-gray-500`. Weekly savings amount uses `text-emerald-600` for consistent color-coding. All numeric values gain `tabular-nums` to prevent layout jitter. Provider section dividers gain `font-semibold` weight.

## [2.3.3] - 2026-05-24

### Changed
- **Dashboard layout: left sidebar → top nav bar**: the sidebar made content cramped on narrower viewports (≤ 1280 px Router / Providers pages got squeezed). The nav is now a single sticky header at the top of the page; brand + version on the left, all 10 nav items in a horizontal row that wraps when needed. Content area is no longer competing with a 224 px aside, so dense pages (Router decision card, Providers cards, Logs rows) get the full viewport width. Removed: the collapse / expand toggle button and the `krouter:sidebar-collapsed` localStorage preference (a vestige of the old aside); `nav.collapse` / `nav.expand` i18n keys are gone too. The Notifications red-badge moves to inline-with-the-nav-label on the top bar.

## [2.3.2] - 2026-05-24

### Changed
- **Free LLM credits promoted from a Dashboard card to a top-level nav item**: the catalogue now lives at `/krouter/free-tokens` with its own page header, subtitle, and proper empty-state — sidebar entry "Free tokens" / "免费 Token" sits between Dashboard and Router with the Gift icon. The old `FreeProvidersCard` component (and its standalone test) are removed; the Dashboard's per-section block is replaced by a one-line comment pointing at the new page. Rationale: the catalogue's discovery / dual-protocol explainer / configured-vs-available split is a substantial feature that deserves a peer-level surface, not a Dashboard sub-card.
- **Subscription providers (MiniMax, …) merged into the Providers page**: each subscription-based provider now gets a purple `SUBSCRIPTION` badge in the collapsed header. Expanding the card surfaces a new **Subscription quotas** section listing every active scenario — each row uses the more standard "X / Y (Z% used)" Western dashboard format with a USED-fill progress bar (emerald → yellow → red traffic-light tones), the local-time window and reset countdown, plus the per-call cost when known. The standalone `SubscriptionQuotaCard` on Dashboard is removed (data shows on Providers); the previous "remaining + reverse-fill bar" representation is gone. A refresh button on each subscription block calls `/internal/subscription/refresh` with the provider name.
- **MiniMax now persists all subscription scenarios, not just `MiniMax-M*`**: `parseQuotaResponse` previously dropped six of the seven scenarios MiniMax's `token_plan/remains` endpoint returns (`speech_synthesis`, `lyric_generation`, `image_generation`, `music_generation`, `mcp_image_understanding`, `mcp_web_search`, plus the original text plan). All are now stored as `subscription_quota_cache` rows with their raw `model_name` as `model_pattern`. A frontend scenario-name mapper recognises the common shapes and shows friendly i18n names ("Text generation" / "文本生成" etc.); unknown names fall through to the raw value, so future scenarios surface immediately without a UI change. Zero-quota / inactive rows are still skipped.

### Fixed
- **Apply Update now actually restarts the daemon**: after successfully replacing the binary on disk, the daemon broadcasts an `update_restarting` SSE event (so the dashboard shows "Restarting…"), waits 300 ms for the event to flush, then uses `syscall.Exec` (Unix) / spawn-and-exit (Windows) to replace the running process with the new binary. Previously the process kept running indefinitely, reporting the old version.

## [2.3.1] - 2026-05-24

### Added
- **HTTP status code explanations in the dashboard**: routing-decision rows, expanded cards, and the Providers "consecutive failure" banner now translate raw codes (`401`, `402`, `429`, `503`, …) into plain English / Chinese so users don't have to memorise what each one means. Every status pill carries a hover tooltip with the code + explanation (e.g. `402 — Payment required: provider quota exhausted or account out of credits`); non-2xx codes also get an inline explanation below the latency line in the expanded view and below the failure-streak count on the Providers card. New helper `frontend/src/lib/statusCode.ts` maps the common codes (200/201/204/400/401/402/403/404/408/409/413/422/429/500/502/503/504) plus 2xx/3xx/4xx/5xx fallback buckets to i18n keys under `status.*`.

### Changed
- **Router decision card redesigned with parallel Request / Response panels + savings banner**: previously the two cards on the Router page used different field sets (left = protocol + model only; right = provider + model + cost) so they didn't read as a true comparison. Now: (1) a **Request** section shows two parallel cards with the same 6 fields each — endpoint, protocol, provider, model, input/output price per 1M tokens, estimated tokens — with the model name and prices visually highlighted because that's what differs between the two; (2) a **Response** section shows two parallel cards — actual tokens (in/out/cached) and actual cost — with the Actual cost prominently larger and highlighted; (3) a **savings banner** below the Response cards reads "Saved $X.XXXX (XX.X%) by routing" with a green trend-down icon, or "Cost $X more (Y%) than the requested model" with a red trend-up icon when routing was more expensive, or a neutral "no savings to report" line when the requested and routed sides match exactly.
- **API: `/internal/logs` JSON + `request_completed` SSE event gain 6 new fields**: `requested_provider`, `requested_input_per_mtok`, `requested_output_per_mtok`, `routed_input_per_mtok`, `routed_output_per_mtok`, `baseline_cost_usd`. All omitempty / optional — legacy daemons or unknown-model records still parse, and the UI falls back to "—" cleanly.
- **`pricing.Service.ProviderFor(model)`**: new public accessor returning the canonical provider name for a given model id.

### Fixed
- **macOS `krouter start` gave cryptic exit-status-5 when plist was missing**: now returns a clear message directing users to run `krouter install` first.
- **Release checksums omitted bare binaries**: `checksums.txt` now includes `krouter-linux-amd64`, `krouter-linux-arm64`, `krouter-apple-macos`, and `krouter-windows.exe` alongside the archives — these are the files the auto-updater manifest points to.
- **v2.0.x upgrade: `pricing_cache` table not renamed on first run**: users upgrading from v2.0.x kept the old `pricing_cache` / `pricing_sync_meta` table names; the daemon now renames them to `token_price_api` / `token_price_api_meta` before running SQL migrations.
- **Provider models endpoint always returned `[]` (regression from v2.3.0)**: `GET /internal/providers/{name}/models` was reading from `model_catalog`, which the daemon never populates from the LiteLLM sync flow — so every Provider card in the dashboard showed "No models catalogued yet" universally, including for builtin providers like `anthropic` and `minimax` that clearly have pricing on file. Fix: read from `token_price_api` (where the LiteLLM sync actually writes), filter by `provider == name`, return rows sorted by `model_id` for stable order, and surface `cached_input_per_mtok` in the response too. The providers-list `model_count` field had the same root cause and is now derived from a new `CountPricesByProvider()` query that returns the grouped count in a single SQL trip.

## [2.3.0] - 2026-05-23

### Added
- **Agent scanners: Cursor, Hermes, OpenCode, Codex CLI (spec/04 Phase 2)**: krouter now auto-inherits endpoint configuration from four more AI coding tools.
  - **Cursor**: reads `~/.cursor/settings.json`; extracts `cursor.anthropic.baseUrl` and `cursor.openai.baseUrl`. API key is left empty (Cursor stores credentials in the OS keychain).
  - **Hermes**: reads `~/.hermes/config.toml`; iterates `[providers.*]` entries; maps `anthropic` → `anthropic-messages`, everything else → `openai-chat`; reads inline `api_key` if present.
  - **OpenCode** (SST opencode.ai): reads `~/.config/opencode/opencode.json` (Linux/macOS) or `%APPDATA%/opencode/opencode.json` (Windows); single-provider flat JSON; `provider` defaults to `"openai"` when absent; `Connect` / `Disconnect` write/delete `baseUrl` in place.
  - **Codex CLI** (OpenAI): reads `~/.codex/config.toml`; resolves the active provider via `model_provider`, reads the API key from the environment variable named by `env_key`; `Connect` injects a `[model_providers.krouter]` entry and sets `model_provider = "krouter"`; `Disconnect` removes the entry and restores the previous active provider.
- **Remote subscription pricing sync (spec/05 §11.4)**: the daemon now polls `https://krouter.kinthai.ai/data/token_price_sub.json` every 24 h with ETag conditional requests, so price changes propagate to running daemons within a day — no binary upgrade needed. The kinthai.ai endpoint is the **primary** channel because operating it ourselves gives us access logs (fleet version distribution, daily unique IPs, 304 vs 200 ratio, geographic spread) that `raw.githubusercontent.com` does not expose to repo owners; the daemon sends a versioned User-Agent (`krouter-subpricing-sync/<version>`) so the access log breaks down by deployed daemon version without any in-product telemetry. Falls back to `https://raw.githubusercontent.com/kinthaiofficial/krouter/main/data/token_price_sub.json` when the primary errors out. Single source-of-truth file: the same `data/token_price_sub.json` is `go:embed`-ed into the installer for the offline-seed path, so the file the daemon fetches at runtime is byte-identical to what shipped with its binary. Schema validation guards against accidental price typos (negative values, zero `total_count`, `monthly_price_cny` > ¥100,000 are all rejected, leaving last-known-good rows intact). New `subscription_pricing_updated` SSE event fires when rows are actually written so the dashboard refetches `/internal/subscription/status` immediately. New `internal/subpricing` package; 16 tests cover happy path, 304 cache hit, primary→fallback failover, both-endpoints-down, schema rejections, versioned UA, and SSE callback wiring.
- **Free LLM credit discovery + automatic routing preference (spec/06)**: krouter now ships with a curated catalog of LLM providers that offer free tokens — DeepSeek (¥10 trial), Groq (永久免费 daily quota), NVIDIA NIM (1000 credits), Qwen / Zhipu / Moonshot / Cerebras / Together / OpenRouter / Mistral / Gemini and more (currently 19 entries, see `data/free_tokens.json`). The dashboard surfaces them as a "Free LLM credits" card with one-click signup links; once a user pastes the API key into any agent (OpenClaw / Claude Code / etc.), krouter detects it via the existing inheritance flow and routing automatically prefers the free provider until the upstream returns 4xx (auth / quota / rate-limit), at which point the provider is marked exhausted for 1 h–24 h and routing falls back to the paid cheapest path. No dashboard configuration required from the user — pasting the key into the agent is the entire workflow.
  - New tables `free_provider_state` (curated catalog) and `provider_exhausted_until` (4xx exhaustion marks, TTL-based) in migration 011.
  - New `internal/freeproviders` package mirrors the spec/05 §11.4 sync pattern: primary `https://krouter.kinthai.ai/data/free_tokens.json`, fallback `https://raw.githubusercontent.com/kinthaiofficial/krouter/main/data/free_tokens.json`, ETag-conditional, 24 h cadence, schema-validated before upsert. Daemon sends `User-Agent: krouter-freeproviders-sync/<version>` so kinthai.ai access logs can break down by fleet version.
  - Routing engine gains `WithFreeProviders(FreeProviderSource)`. When set, `cheapestProviderModel` consults the free list first (provider in `free_provider_state` ∩ `inherited_endpoints` ∩ not exhausted, protocol matches) and falls through to the paid cheapest path only when no free candidate qualifies.
  - Proxy's 4xx path now marks the provider exhausted: 401/403 → 1 h, 402 → 24 h, 429 → 5 min. Marks are harmless for paid providers (routing doesn't consult the table for non-free routes) and let free-credit users see "exhausted" badges on the dashboard with the upstream's status code as the reason.
  - New `/internal/free-providers` API joins the catalog with `inherited_endpoints` so dashboards can show "claimed / not yet claimed" state. Tests: 38 across 5 packages (storage / freeproviders / api / routing / cmd-krouter freeProviderSource / frontend FreeProvidersCard).
- **Dual-protocol free providers (follow-up to spec/06)**: catalog entries now carry an optional `additional_protocols` array for vendors that expose more than one protocol endpoint on the same account — OpenRouter, GLM/Zhipu, and Moonshot all offer an Anthropic-compatible `/messages` alongside the OpenAI-compatible `/chat/completions`. Each alternate has its own `krouter_provider_name` because the user has to configure the alternate as a separate provider entry inside their agent (same API key, different baseURL). Routing's protocol filter is fully preserved (spec/00 §B2): a new `FreeProviderKrouterNamesByProtocol` returns a per-protocol map, so an anthropic-protocol request only sees the anthropic-side krouter names and never cross-routes protocols even on dual-protocol vendors. The dashboard surfaces a top-of-card explainer about the same-protocol invariant plus a per-row indigo hint panel on dual-protocol rows showing the alternate `protocol → krouter_provider_name` pair with the setup hint and a configured/未配置 badge. Migration 012 adds the `additional_protocols_json` column to `free_provider_state` (pure additive ALTER — pre-existing rows keep `[]` and continue as single-protocol). Adds 8 dual-protocol tests across storage / sync schema validation / routing source / frontend FreeProvidersCard.

- **Router page in the dashboard**: new top-level nav item (between Dashboard and Agents) that surfaces routing decisions as they happen. The most-recent decision is shown as a large card with a requested-model → routed-result diff (two side panels showing protocol + model on the left and provider + routed model + cost on the right, arrow between them, highlighted green when the model actually changed). Older decisions collapse under a single "N earlier" toggle; each row can be individually expanded to the same large card view. Seeds from the last 50 records in `/internal/logs` and live-extends from the `request_completed` SSE channel — a green "live" pill with a pulsing dot indicates the SSE connection is open; new arrivals briefly pulse a green border on the latest card for ~1.5 s. The Logs page remains the searchable / paginated archive of the same data; this new page is the always-visible "what just happened" view.
- **i18n key `router.*`**: new strings (title, subtitle, live/offline, waiting, agent_requested, krouter_routed, protocol/model/provider/cost, tokens_breakdown, latency_ms, status, request_id, saved_vs_requested, no_change, show_n_more, hide_history, latest_badge) in both `en.json` and `zh.json`.

### Changed
- **Nav rename: "Announcements" → "Notifications" / "通知"**: the existing announcements feature is now surfaced as Notifications in both English and Chinese. Page title and the empty-state copy follow the same change; storage and API paths are unchanged so this is a strict UI rename with no behaviour or DB impact.
- **Collapsible sidebar**: a small toggle button at the bottom of the left sidebar shrinks it to an icon-only rail (56 px wide). Labels hide; the active nav item still highlights; the Notifications unread badge degrades to a tiny red dot at the icon's top-right corner. State is persisted in `localStorage` under `krouter:sidebar-collapsed` so the preference survives reloads. Smooth 200 ms width transition.
- **Removed: bottom-bar `proxy :8402` footer and its divider line** from the sidebar. The version was the only useful bit there and it already sits beside "KRouter" in the header; the port number rarely changes after install and now lives only in the About page.
- **Logs page now mirrors the Router page schema exactly**: replaces the fixed-column table with the same expandable row component the Router page uses. Each row collapses to a one-line summary (time · agent · requested → provider/routed · cost · latency · status pill) and expands inline to the full Router-style card with the requested→routed diff, protocol, cost, and the full tokens breakdown (`in · out · cached`). New protocol filter dropdown next to the agent filter; the search box now also matches against `requested_model` and request `id`. CSV export gains `protocol`, `requested_model`, `cached_tokens` columns. The Router-page card components moved to `frontend/src/components/RoutingDecision.tsx` so both pages share the same source of truth — Logs is now strictly Router's "search & archive" view.
- **API: `/internal/logs` JSON gains `cached_tokens`** (and so does the SSE `request_completed` event). The column already existed in `request_records.cached_tokens` from before — the API DTO was just not exposing it. Purely additive; existing clients ignore the new field.
- **Budget promoted from a Settings section to its own top-level page**: new "Budget" / "预算" nav item (Wallet icon) between Providers and Settings. The page shows: today's spend bar (with 80% / 95% / 100% colour thresholds and a "Blocked" badge), spent / limit / remaining / requests stats, daily & weekly limit inputs (edit-in-place; commits on blur or Enter), savings-today copy when applicable, and a "Recent threshold events" timeline pulled from a new `budget_events` table. SSE `budget_warning` invalidates the page's queries live.
- **Migration 013 `budget_events`**: persists each 80% / 95% / blocked / unblocked transition the budget monitor goroutine detects. Previously these were SSE-broadcast-only and lost on disconnect; now they survive restarts and can be reviewed on the Budget page. Index on `ts_utc DESC` for the recent-events scan. The monitor's existing dedup means at most a few rows per day per threshold per UTC day.
- **API: `GET /internal/budget/events?limit=N`** (default 50, capped at 500) returns recent budget transitions newest-first.
- **Settings page cleanup**: removed Routing Preset, Desktop Notifications, Pricing Data, and the inline Budget Limits section. Settings now contains only Language and Data Management (export logs by date range, reset data, uninstall). The removed sections weren't deleted from the backend — preset / notification_categories / pricing/status endpoints still exist and other places (CLI, install wizard) may still use them — only the dashboard surface area is trimmed.
- **Providers page expanded with endpoint / models / pricing / lifetime usage**: each provider is now a click-to-expand card. Header shows display name, protocol badge, builtin chip, and three summary chips (model count · lifetime requests · lifetime cost). Expanding reveals: base URL, path prefix and full endpoint (3 separate rows so they're individually copy-able); today's success rate / requests / cost / p50-p95 latency; lifetime in / out / cached tokens; the full **per-model price table** (`model_id`, input/M, output/M, max tokens) pulled from `model_catalog` via a new endpoint; a failure-streak banner when consecutive_failures > 0; and Test / Set Key / Remove buttons. The page no longer reorders providers by current request usage — that live "which provider is in use" view belongs on the Router page.
- **API: `GET /internal/providers` JSON gains** `path_prefix`, `requests_total`, `input_tokens_total`, `output_tokens_total`, `cached_tokens_total`, `cost_total_usd`, `model_count`. Purely additive.
- **API: `GET /internal/providers/{name}/models`** returns the per-provider model catalog with per-1M-token pricing (computed from `model_catalog.input_cost_per_token × 1_000_000` etc.) and `max_tokens`. Same auth as the rest of `/internal/*`.
- **Storage: `ProviderTokenTotalsSince(provider, since)`** sums input / output / cached tokens + cost + count for a provider since a given time. Use `time.Time{}` for lifetime totals.

### Fixed
- **Test fix in `TestAgentAction_LegacyVerbsStillReachableUnderNewDispatch`**: previously asserted that POST `/internal/agents/openclaw/connect` returned `"agent not found"` to prove dispatch reached the handler. Failed on developer machines that have OpenClaw actually installed (the handler then succeeded with `{"ok":true}`). Switched to a guaranteed-not-installed agent name (`nonexistent-vendor-xyz`) so the assertion remains host-independent.

## [2.2.0] - 2026-05-23

### Added
- **Daily budget hard stop**: routing engine now checks `DailyPercent >= 1.0` before selecting a provider. When the limit is reached, `Decision.BudgetExceeded` is set and the proxy returns HTTP 429 — Anthropic-protocol callers receive `{"type":"error","error":{"type":"rate_limit_error",...}}`, OpenAI-protocol callers receive `{"error":{"type":"insufficient_quota",...}}`. Requests are blocked until the spend counter resets at midnight UTC.
- **Budget monitor goroutine**: `monitorBudget` runs a 60 s ticker in `serve.go` and broadcasts SSE events (`budget_warning`) at 80%, 95%, and 100% of the daily limit. Each threshold fires at most once per day (dedup via last-fired timestamp).
- **Dashboard budget bar**: new `BudgetBar` component shows a live spend-vs-limit progress bar that turns yellow at 80%, red at 95%, and adds a "Blocked" badge with an unblock hint when spending is stopped.
- **Install wizard budget step**: new `BudgetStep` page added between "Shell" and "Done". Defaults to $50/day; calls `POST /api/install/set-budget` on the install server so the limit is written to `settings.json` before the daemon starts for the first time.
- **Default $50/day limit**: `applyDefaults` in `internal/config/manager.go` sets `budget_warnings["daily"] = 50` when the key is absent, so out-of-the-box installs are protected without requiring the wizard step.
- **UI localization (English / Chinese)**: all user-visible strings in the dashboard and install wizard are extracted to `frontend/src/locales/{en,zh}.json` and `frontend-install/src/locales/{en,zh}.json` using `react-i18next`. Language is detected from `navigator.language` during install and from `localStorage` thereafter; the Settings page lets users switch at any time and syncs the choice to `settings.json` immediately.

### Changed
- `GET /internal/budget` response now includes `daily_limit_usd`, `daily_percent_used`, and `budget_blocked` when a daily limit is configured.
- Currency display unified to USD across both English and Chinese locales — no per-locale currency formatting.

## [2.1.0] - 2026-05-22

### Fixed
- **Subscription routing could pick the wrong tier when minimax has multiple plans (spec/05 §8)**: `subscriptionSource.GetSubscriptionInfo` iterated `GetAllSubscriptionQuotas` and returned the first `IsAvailable()` row, then hardcoded the rewrite-target model as `MiniMax-M2.7` regardless of which tier had matched. If a user's `MiniMax-M*` (LLM) tier was exhausted but `speech-hd` (TTS) still had quota, routing thought minimax was available, sent an LLM request as `MiniMax-M2.7`, and minimax replied 4xx because the M* tier was empty. Fixed by explicitly looking for the tier whose `model_pattern` wildcard-matches the rewrite target (`storage.SubscriptionQuota.MatchesModel` using `path.Match`). Standard tier preferred over highspeed; highspeed used only as fallback. New tests cover the speech-hd masking scenario, standard-vs-highspeed precedence, the highspeed fallback path, and the non-minimax provider case.

### Added
- **`subscription_exhausted` SSE event (spec/05 §12.3)**: when the MiniMax quota poller observes a tier transitioning from "had quota" to "zero remaining" within the current window, the daemon broadcasts a `subscription_exhausted` event with `{provider, tier, highspeed, window_end}`. Dedupe key is `window_end` so we don't refire repeatedly within the same exhausted window. The dashboard `SubscriptionQuotaCard` listens for the event, surfaces a transient banner ("quota exhausted, routing fell back to per-token vendors until window reset at …"), and force-refetches `/internal/subscription/status` so the bars update immediately instead of waiting 60s for the next poll.

### Changed
- **MiniMax subscription card now shows the original CNY price**: the dashboard formerly displayed e.g. `$6.76/mo plan`, which felt odd to users who paid `¥49/月` on minimaxi.com. It now shows `¥49/mo (≈ $6.76)`. The USD value is still computed (CNY × 0.138 fixed rate) so users can compare costs against per-token vendors. New `monthly_price_cny` field on `/internal/subscription/status` carries the original number.
- **`agentscan.PendingFileDir()` now resolves to `~/.kinthai/` only** (was: `$XDG_CONFIG_HOME/krouter` → `~/Library/Application Support/krouter` → `~/.config/krouter` fallback chain). This fixes a silent-data-loss bug where the installer running from a shell with `XDG_CONFIG_HOME` set would write `pending-agents.json` somewhere the launchd-started daemon never looks (LaunchAgent plists inject `HOME` but not arbitrary shell env vars). New regression test asserts the path is invariant under `XDG_CONFIG_HOME` changes. `KROUTER_CONFIG_DIR` continues to override (for tests and site-specific deployments).
- **`GET /internal/agents` is now a unified inheritance + legacy view (spec/04 §8.6)**: the existing JSON shape (`name`, `connected`, `providers`, `stats`) is preserved, but the response is now unioned with the Scanner registry plus `agent_settings` / `inherited_endpoints` so a dashboard refresh sees everything in one round-trip. New optional fields (all `omitempty`, so older clients ignore them): `supported` (Scanner compiled in for this agent), `enabled` (agent_settings.enabled), `inherited_count`, `last_scanned_at`, `last_error`. Scanner-registered agents that aren't yet installed on disk still appear (so the dashboard can show "OpenClaw (not installed)" rows); v2.0.47-style detection-only agents (Cursor / Hermes) still appear with `supported=false` until their Scanner ships.

### Added
- **Auto-rescan + SSE on MiniMax OAuth failure (spec/05 §15.2)**: when the `QuotaPoller` sees the token rejected — either via HTTP 401/403 or via the body's `base_resp.status_code = 1004` ("login fail") that the token-plan endpoint returns as HTTP 200 — it now invokes a registered `UnauthorizedCallback`. The daemon wires that callback to `agentscan.RunAll` (so any fresher OAuth token OpenClaw has written to `auth-profiles.json` since the last scan gets picked up automatically) and broadcasts a `subscription_unauthorized` SSE event so the dashboard can surface a "re-login OpenClaw" prompt if the rescan didn't help. New `ErrMinimaxAuth` sentinel error lets callers `errors.Is(err, ErrMinimaxAuth)` instead of parsing message strings.


### Added
- **Periodic agent rescan (spec/04 §14 "Hot reload")**: the daemon now runs `agentscan.RunAll` every 5 minutes in addition to the startup-time pass, so config changes the user makes to OpenClaw / Claude Code between daemon restarts are picked up without manual intervention. After each tick the daemon broadcasts an `agents_changed` SSE event with `source: periodic_rescan` so the dashboard can refetch `/internal/agents/configured` immediately rather than waiting for its own react-query refetch interval. Implemented as `agentscan.StartPeriodicRescan(ctx, store, logger, interval, onTick)` — exported so tests (and future fsnotify variants) can drive it with a custom cadence.


### Added
- **`spec/00-design-principles.md`**: distilled cross-cutting design rules (product positioning, transparent-proxy invariants, data architecture, UX rules, engineering trade-offs, process meta-rules). New PRs and reviews are expected to reference principles by their letter+number (e.g. "violates C2 — single source of truth"). Compiled from the working-session history that produced `spec/04` and `spec/05`; each principle ships with the concrete incident or trade-off that motivated it, so a future contributor can decide whether the rule still applies when their case looks different.


### Added
- **Agent inheritance flow (spec/04)**: krouter now auto-extracts vendor endpoints, API keys, and OAuth tokens from the user's already-configured AI agents (OpenClaw, Claude Code) and persists them to a new `inherited_endpoints` table. Wizard gains an "Agent Paths" step; Dashboard gains an inheritance section. See `spec/04-agent-inheritance.md`.
- **Subscription quota dashboard (spec/05)**: new `/internal/subscription/status` and `/internal/subscription/refresh` endpoints; Dashboard gains a MiniMax subscription card showing effective cost, monthly price, and per-tier window-reset countdown. See `spec/05-subscription-quota.md`.
- **Scanner architecture (`internal/agentscan`)**: one Go file per AI agent implementing the `Scanner` interface (`AgentID`, `DisplayName`, `DefaultConfigPath`, `Scan`). Adding a new agent is purely additive — write the file, append the value to the registry — no schema change, no manifest layer.
- **Injectable MiniMax `QuotaPoller.WithTokenResolver`**: the poller now reads the OAuth token from `inherited_endpoints.extras_json` first, closing the cold-start gap where the daemon couldn't poll quota until the user had sent a first proxied MiniMax request.
- **`resolveProviderKey` helper**: routing engine and model discovery resolve vendor API keys through a single helper with precedence `inherited_endpoints > settings.ProviderKeys > env var`. Vendors the user already configured in OpenClaw no longer require a second key entry in the dashboard Settings page.

### Fixed
- **MiniMax pricing — duplicate tables disagreed (caught in PR review)**: an earlier commit in this PR introduced `internal/providers/minimax/pricing.go` as an independent lookup table (USD prices, formula `monthly_price_usd / total_count`) that conflicted with the existing `internal/storage/subscription_quota.go::minimaxPlanPriceCNY` (CNY prices, formula multiplied by `windows_per_month`). Worst case: the dashboard reported an effective cost roughly 1043× the value the routing engine was using; the SKU `{600, true}` was treated as "free" by routing while the dashboard quoted $0.0483/call. Fix: deleted `internal/providers/minimax/pricing.go` outright, routed all callers through `storage.SubscriptionQuota.EffectiveCostUSD()` and `MonthlyPriceUSD()` (single source of truth), corrected the `spec/05 §11` formula to include the `windows_per_month` factor, and added the pricing matrix + call graph + bug history to the spec as a cautionary note for future contributors.

## [2.0.50] - 2026-05-19

### Added
- **数据驱动 Provider 架构**：新增 `provider_config` DB 表（migration 007），预置 15 个 provider 的元数据（name、display_name、base_url、path_prefix、protocol、is_builtin）。daemon 启动时从 DB 加载所有 OpenAI 兼容 provider，不再需要修改代码才能支持新 provider。
- **新增 7 个内置 provider**：Google Gemini（`/v1beta/openai` 兼容端点）、xAI Grok、Mistral AI、Together AI、Fireworks AI、Perplexity、Ollama（本地）。加上现有 8 个，总计 15 个内置 provider，均通过 API key 配置即可激活。
- **自定义 provider CRUD**：`POST /internal/providers`（创建自定义 provider）、`DELETE /internal/providers/:name`（删除自定义 provider）。支持热添加/删除，无需重启 daemon。
- **Providers 页面 UI 升级**：显示 display_name 和 base_url（来自 DB），"Add Provider"对话框支持填写 name/base_url/api_key/path_prefix/protocol 添加任意 OpenAI 兼容端点；每个未配置 provider 卡片内联"Set Key"输入框，可直接在 Providers 页设置 API key；自定义 provider 显示删除按钮。
- **provider_keys 校验放开**：`PATCH /internal/settings` 的 provider_keys 从静态白名单改为格式校验（小写字母开头 + 字母数字 + `-_`），允许为任意 provider 设置 key。
- **LiteLLM 映射扩展**：`LiteLLMToKrouterProvider` 新增 `together_ai→together`、`fireworks_ai→fireworks`，LiteLLM 同步的模型定价可自动关联到对应 provider。

### Changed
- `serve.go` 移除了 deepseek、groq、moonshot、glm、qwen、openai 六个硬编码适配器注册，改为 `loadProvidersFromDB()` 统一加载。现有 per-provider 包（用于测试和文档）保持不变。
- `GET /internal/providers` 响应新增 `display_name`、`base_url`、`is_builtin` 字段。

## [2.0.49] - 2026-05-19

### Fixed
- **Bug F（P1）— Claude streaming token 永远 0 — 根因修复**：Anthropic API 对发送了 `Accept-Encoding: gzip` 的请求返回 gzip 压缩响应；krouter 直接把压缩字节写入 SSE capture buffer，`parseAnthropicSSEUsage` 永远解析失败。修复：在 Anthropic 适配器的 `Forward` 方法里删除上游请求的 `Accept-Encoding` 头，强制返回未压缩文本。代价：每次响应多几 KB 流量，换取 token 统计可靠性。
- **Bug Q — installer 端口 8404 静默切换**：`stopRunningDaemon()` 只停 daemon service，不杀残留的老 installer 进程；老 installer 占用 8404 时新 installer 静默切到 8405，用户/脚本基于固定端口假设的操作全部 noop。修复：启动前新增 `stopRunningInstaller(8404)` — 先 TCP 探测端口是否占用，若占用则用系统命令（macOS: `lsof -ti | xargs kill -9`，Linux: `fuser -k`，Windows: `netstat + taskkill`）强制杀掉。

## [2.0.48] - 2026-05-19

### Fixed
- **Bug P（P0）— migration 005 UNIQUE 冲突致 daemon 启动失败**：`005_rename_glm_to_zai.sql` 在 glm 和 zai 行并存时触发 `UNIQUE constraint failed`，daemon 无法启动。修复：先删除与已有 zai 行冲突的 glm 行，再执行 UPDATE；对已完成 migration 的 DB 无影响（`schema_migrations` 版本记录存在则跳过）。

### Added
- **Bug F 诊断工具**：proxy 层在每次 Anthropic 流式响应结束后，将 SSE raw buffer（最多 4 KB）存入 `Server.lastSSECapture`；当 `parseAnthropicSSEUsage` 返回 0/0 token 时，debug 日志打印前 512 字节供排查。新增 `GET /internal/debug/last-sse-capture`（需 Bearer auth）端点，返回上次捕获的原始 SSE bytes，方便研发判断流内容是否包含 `message_start` / `message_delta` 事件。

## [2.0.47] - 2026-05-19

### Added
- **Dashboard 周报统计**：新增 `GET /internal/dashboard/stats` 端点，返回近 7 天请求数、花费、节省金额、provider 分布及已连接 agent 数量；Dashboard 页面新增"This Week"汇总卡片和 provider 分布饼图（recharts）。
- **Provider 今日统计**：`GET /internal/providers` 响应新增 `requests_today`、`cost_today_usd`、`latency_p50_ms`、`latency_p95_ms` 字段，Providers 页面展示今日请求量和延迟百分位。
- **Provider Test 按钮**：新增 `POST /internal/providers/{name}/test` 端点，向 provider 发起轻量连通性探测（Anthropic 测 API 可达性，OpenAI-compatible 测 key 有效性）；Providers 页面每个卡片增加 Test 按钮，实时显示延迟和 HTTP 状态码。
- **Agent 配置 diff 预览**：新增 `POST /internal/agents/{name}/diff` 端点，返回 connect 前后的配置 JSON diff（不写文件）；Agents 页面在 OpenClaw 点击 Connect 时先弹出 before/after 对比模态框，确认后再执行。
- **Agent 备份管理**：新增 `GET /internal/agents/{name}/backups` 和 `POST /internal/agents/{name}/restore` 端点；Agents 页面每个已知 config-path 的 agent 卡片底部增加可折叠"Backups"面板，列出所有备份并支持一键还原。
- **日志日期范围过滤**：`GET /internal/logs` 新增 `?from=YYYY-MM-DD&to=YYYY-MM-DD` 参数；Logs 页面新增两个日期选择器，设置后按范围过滤记录。
- **日志 CSV 导出接口**：新增 `GET /internal/logs/export?from=&to=` 端点，返回 CSV 文件下载；Settings 页面新增日期范围输入 + Export CSV 链接。
- **数据管理**：新增 `POST /internal/settings/reset-data`（清空请求历史）、`POST /internal/settings/uninstall`（断开所有 agent 连接）；Settings 页面新增"Data Management"分区含上述两个操作按钮。
- **通知类别扩展**：`HandleEvent` 新增四种通知类型：`free_credit`（免费额度到账）、`provider_news`（provider 更新）、`tip`（节省建议）、`critical_warning`（严重告警）；Settings 页面通知偏好列表同步补全这四项。
- **`Pinger` 接口**：`providers` 包新增 `Pinger` 可选接口；Anthropic 和 OpenAI 适配器各实现 `Ping(ctx) (latencyMS, statusCode, err)` 方法。

## [2.0.46] - 2026-05-19

### Added
- **5xx Fallback 链**：proxy 层新增 `tryWithFallback`，当 provider 返回 5xx 或网络错误时自动调用 `engine.FallbackDecide`，最多重试 2 次；Anthropic 协议按 opus→sonnet→haiku 降档，OpenAI 协议切换到其他同协议 provider；4xx（401/429）直接透传，不触发重试。
- **复杂度评分（`ComplexityScore`）**：替换原来的布尔判断（`HasImages || HasTools && tokens>4000`），综合图片、大 token 量、HasTools 组合、system prompt 关键词（debug / refactor / architect 等 10 个）计算 0.0~1.0 分值；Quality preset 以 ≥0.4 为复杂请求阈值，决定是否升级到 Opus。
- **`EstimatedCostUSD` 填充**：`enrichDecision` 在 routing engine 内部自动计算路由决策的预估花费，填入 `Decision.EstimatedCostUSD`；当路由模型比请求模型便宜时，在 `Reason` 末尾追加"比 X 便宜 Y%"。
- **per-agent 路由覆盖**：`settings.json` 新增 `routing_overrides` 字段，支持按 agent 名（openclaw / claude-code / cursor / unknown）配置 `always_use`（强制使用特定模型）或 `preset`（覆盖活跃 preset）；engine 通过新增的 `OverrideSource` 接口注入，daemon 启动时由 `config.Manager` 实现。

## [2.0.45] - 2026-05-19

### Added
- **请求元数据提取（HasImages / SystemPrompt）**：proxy 层在构建 `routing.Request` 前调用 `extractAnthropicMeta` / `extractOpenAIMeta`，解析请求体中的 `system` 字段和图片内容块，填充 `HasImages`、`SystemPrompt` 字段。路由引擎可据此在订阅优先路由时跳过含图片请求（MiniMax 不支持）。
- **Anthropic 预算限制自动降级**：routing engine 新增 `QuotaSource` 接口和 `applyQuotaDowngrade` 逻辑；当每日/每周花费占比 ≥ 95% 时强制切换 Saver preset，≥ 80% 时将 Quality→Balanced、Balanced→Saver；`OpusPercent`（基于 500K token 软上限的窗口消耗比）≥ 80% 时屏蔽 Opus 系列，降级使用 Sonnet。
- **`QuotaSource` / `SubscriptionSource` 接口**：routing engine 通过 `WithQuota(QuotaSource)` 和 `WithSubscription(SubscriptionSource)` 注入依赖，保持路由层与存储层解耦，便于测试替换。

## [2.0.44] - 2026-05-19

### Added
- **MiniMax 订阅 provider 优先路由**：routing engine 新增 `SubscriptionSource` 接口，当 MiniMax 订阅配额有剩余且请求不含图片时，三个 preset（Saver / Balanced / Quality 非复杂）均优先路由到 MiniMax，利用"月费÷月总次数"极低的有效成本（≈$0.000031/次）覆盖大多数请求。
- **MiniMax 配额轮询（`QuotaPoller`）**：每 30 分钟调用 `https://api.minimaxi.com/v1/token_plan/remains`，当前窗口剩余 < 30 分钟时缩短为 5 分钟；结果存入新 DB 表 `subscription_quota_cache`；daemon 启动后 20 秒触发首次拉取。
- **OAuth token 缓存**：proxy 在每次 MiniMax 请求中提取 `Authorization: Bearer` 头并存入内存（`minimax.CacheOAuthToken`），供 QuotaPoller 调用配额 API 时使用；token 不写磁盘、不打印日志。
- **`SubscriptionQuota` 存储层**：新增 `internal/storage/subscription_quota.go`，包含 `UpsertSubscriptionQuota`、`GetSubscriptionQuota`、`GetAllSubscriptionQuotas`、`IsSubscriptionAvailable` 方法；`EffectiveCostUSD()` 按订阅套餐价格表计算每次调用等效美元成本。
- **DB migration `006_subscription_quota.sql`**：创建 `subscription_quota_cache` 表。

## [2.0.43] - 2026-05-19

### Fixed
- **Bug F（Anthropic streaming token 为 0）**：`parseAnthropicSSEUsage` 和 `parseAnthropicJSONUsage` 改用 JSON 解析替代正则表达式，累加 `input_tokens + cache_creation_input_tokens + cache_read_input_tokens` 作为总 input token 数，修复了 prompt caching 激活时 `input_tokens` 字段为 0 导致统计清零的问题。`cache_read_input_tokens` 作为 `cachedTokens` 传入定价函数，享受折扣费率。
- **Bug O（glm→zai 重命名遗漏）**：新增 DB migration `005_rename_glm_to_zai.sql`，将 `model_discovery` 表中旧的 `provider='glm'` 行批量改为 `'zai'`；新增 `config.Manager.MigrateKeys()` 在 daemon 启动时自动将 `settings.json` 中的 `provider_keys.glm` 迁移为 `provider_keys.zai`，避免老版本升级后 API key 失效。
- **Bug L（settings 接受未知 vendor）**：`PATCH /internal/settings` 的 `provider_keys` 字段现在校验 key 是否合法（`deepseek / groq / moonshot / zai / qwen / openai / minimax`），传入未知 key 时返回 400 Bad Request，附带可接受 key 列表。

### Added
- **Bug K（OpenAI 直连 provider）**：注册 `openai` provider（`https://api.openai.com`，`OPENAI_API_KEY`），支持 `gpt-4o`、`gpt-4o-mini`、`gpt-4-turbo`、`gpt-4`、`o1`、`o1-mini`、`o3-mini`。兑现 README 承诺。

## [2.0.42] - 2026-05-19

### Added
- **全量模型 Catalog（`model_catalog` 表）**：LiteLLM JSON 每 24h 同步时，将所有模型条目（含 cost=0 的）写入新的 `model_catalog` 表，字段包括 `litellm_provider`、`model_id`（去掉 provider 前缀）、`raw_key`、定价与 `max_tokens`。此表作为全平台大模型目录的权威来源。
- **`pricing.LiteLLMToKrouterProvider` 映射表**：声明 LiteLLM provider 名称与 krouter adapter 名称不同的对应关系（目前仅 `dashscope → qwen`；其余在两侧名字一致后直接对应）。
- **`providers.ModelSetter` 接口**：允许在运行时替换 adapter 的 `SupportedModels()` 列表。`anthropic` 和 `openai`（含所有 OpenAI-compatible 子适配器）均实现此接口。
- **`pricing.Service.OnSync` 回调**：每次 LiteLLM 同步完成后调用，传入按 `litellm_provider` 分组的模型列表。`serve.go` 注册此回调，自动将 catalog 模型列表推送到对应 adapter，无需重启 daemon。
- **新 DB migration `004_model_catalog.sql`**：创建 `model_catalog` 表及 `idx_model_catalog_provider` 索引。
- **`storage.Store.UpsertModelCatalogBatch` / `GetModelsByLiteLLMProvider` / `GetAllModelCatalog`**：新增 catalog 相关 storage 方法。

### Changed
- **GLM adapter 改名 `glm` → `zai`**：跟进智谱品牌重命名（Z.AI），与 LiteLLM 和 OpenClaw 命名保持一致。settings key 同步改为 `zai`（旧配置需手动更新 `ZHIPU_API_KEY` 对应的 settings 键名）。静态模型列表补充新版 `glm-4.5` 系列。
- **Moonshot adapter 改名 `moonshot-cn` → `moonshot`**：与 LiteLLM 和 OpenClaw 命名保持一致。静态模型列表补充新版 Kimi 系列（`kimi-k2.5`、`kimi-k2.6`、`kimi-latest` 等），保留旧版 `moonshot-v1-*` 作为 fallback。

## [2.0.41] - 2026-05-19

### Added
- **Settings → Pricing 页面（spec/04-pricing.md §8）**：Settings 页面新增 Pricing Data 区块，展示：
  - 最近同步时间、数据来源徽标（`live` / `cache` / `static`）、已追踪模型数量
  - 本月总花费与节省金额（micro-USD 精度）
  - 过去 30 天 Top 10 模型使用表格（请求数、花费、输入/输出每百万 token 价格）
- **`GET /internal/pricing/status`** 管理 API 端点：返回同步元信息、模型统计、本月聚合花费及 30 天热门模型列表。
- **`pricing.Service.ModelCount() int`**：返回当前内存价格表中的模型数量，供 API 层展示。
- **`pricing.Service.PriceFor(model string) (inputPerMTok, outputPerMTok float64)`**：按模型返回每百万 token 的输入/输出价格，便于前端展示不做单位换算。
- **`storage.Store.TopModelsByUsage(ctx, since, limit)`**：按请求数降序汇总指定时间窗口内的模型使用统计。
- **`storage.Store.ListRequestsSince(ctx, since, limit)`**：按时间段查询请求记录，用于月度节省计算。

## [2.0.40] - 2026-05-19

### Added
- **价格 tier 驱动路由（spec/04-pricing.md §5）**：routing engine 现在通过 `WithPricing(PricingSource)` 接收 live 价格数据，在 Saver / Quality preset 下动态选择最便宜/最贵的 (provider, model) 对，不再依赖硬编码模型名。
  - **Saver preset**：`cheapestProviderModel` 遍历所有健康且有 key 的同协议 provider，按 `InputCostPerToken` 选最低价模型。若 DeepSeek 比当前注册的 OpenAI gpt-4o-mini 更便宜，saver 自动选 DeepSeek。
  - **Quality preset（复杂请求）**：`mostExpensiveProviderModel` 选最贵模型作为能力最强的升级目标，代替硬编码 `claude-opus-4-5`。
  - **向下兼容**：pricing 未注入（`nil`）或模型不在价格表时，自动回退到原有硬编码逻辑，现有行为不变。
- **`pricing.Service.InputCostPerToken(model string) float64`**：新增公开方法，供 routing engine 按模型查询价格，无需构造完整 token 计数。
- **`routing.PricingSource` 接口**：仅暴露 `InputCostPerToken`，最小化 routing → pricing 的依赖，便于测试时替换。

## [2.0.39] - 2026-05-19

### Fixed
- **pricing: 同步请求不走 OS 代理**：`pricing.Service` 之前用硬编码 `&http.Client{}` 访问 `raw.githubusercontent.com`，在国内受限网络下每 24h 同步必超时。新增 `WithHTTPClient(*http.Client) *Service` 方法；`serve.go` 在创建 `bgTransport`（与 notifications/upgrade 共享的代理感知 transport）后立即注入，然后才调用 `StartSync`。
- **pricing: `raw_json` 列有 schema 但从不写入**：`syncOnce` 写 `UpsertPrice` 时缺失 `RawJSON` 字段，导致 `pricing_cache.raw_json` 始终为空字符串，无法用于调试。修复：`parseLiteLLM` 返回内部 `parsedEntry{PriceEntry, RawJSON}` 结构，将原始 JSON bytes 一并传到 `UpsertPrice`；内存价格表（`prices` map）仍只保存 `PriceEntry`，内存占用不变。

## [2.0.38] - 2026-05-19

### Fixed
- **Bug H — 首次安装用户 `anthropic.models=[]`**：`RefreshModelsIfStale`（daemon 启动 +10s 执行）之前只处理 settings 里有 key 的 provider，跳过了 Anthropic（key 在 openclaw.json 里，不在 krouter settings 里）。修复：`RefreshModelsIfStale` 现在也会检查 OpenClaw 是否已连接，若 anthropic 缓存为空或超过 24h，则调用 `discoverOpenClawModels`，首次安装用户的模型选择器将在 daemon 启动约 10s 后自动填充。
- **Bug I — `minimax-portal.models` 永远 `[]`**：`discoverOpenClawModels` 之前只处理 anthropic provider，从不更新 OpenClaw 的 minimax-portal 节点。修复：新增 `discoverOpenClawMiniMax`，若 OpenClaw 配置中存在 `minimax-portal` 节点则自动写入模型列表——优先通过 `minimax-portal.apiKey` 做实时探测，若 key 不存在则写入 adapter 的静态模型列表（`MiniMax-M2.7`、`MiniMax-M2.7-highspeed`），确保 model selector 始终非空。同时新增 `ReadOpenClawProviderAPIKey(configPath, providerName)` 通用函数（`ReadOpenClawAPIKey` 成为其包装）。
- **Bug J — daemon 内部服务（announcements/upgrade）不走 OS 系统代理**：`notifications.Service` 和 `upgrade.Service` 各自创建了硬编码 `&http.Client{}`，不使用 `proxycfg.Manager` 探测的代理，导致国际网络受限环境下持续 timeout。修复：两个 service 新增 `WithHTTPClient(*http.Client) *Service` 方法，`serve.go` 在 daemon 启动时注入共享的代理感知 transport（`Proxy: proxymgr.ProxyFunc()`），upgrade 15s 超时、notifications 30s 超时，均通过 `proxymgr.ProxyFunc()` 闭包自动感知代理变更。

## [2.0.37] - 2026-05-19

### Fixed
- **Bug G — Saver preset 在多 provider 环境下误路由到 MiniMax**：当同时注册了 Anthropic 和 MiniMax（均使用 Anthropic 协议）时，`decideSaver` 之前调用 `pickHealthyProvider` 可能返回 MiniMax，再把 `claude-haiku-4-5-20251001` 这个 Claude model ID 发给它——MiniMax 不识别该 ID，请求静默失败。修复：改用 `pickProviderForModel(proto, saverAnthropicModel)`，优先选择在 `SupportedModels()` 中明确列出该 haiku 型号的 provider；若回退 provider 不支持该型号，保持原始请求 model 不变。
- **Bug F — Anthropic streaming 请求 latency 统计错误（总是显示 6ms）**：`streamSSEWithCapture` 的 `done` 回调之前会传入自身内部计时的 `latencyMS`，该计时在首字节已到达后才开始，导致记录的永远是流写入耗时（≈6ms）而非真实端到端延迟（如 7000ms）。修复：移除 `done` 回调中的 `latencyMS` 参数，两处调用处改为 `time.Since(start)` 计算从请求到达时刻起的完整延迟。
- **Bug F — Anthropic streaming token 捕获失败（长响应丢失 `message_delta`）**：之前只保留头部 256KB，对于超过此大小的长响应，末尾的 `message_delta`（含 `output_tokens`）会被丢弃，导致 token 统计 output=0、cost=0。修复：改为 64KB 头部缓冲区 + 4KB 尾部滑动窗口，始终保留响应末尾内容，确保 `message_delta` 不丢失，同时大幅降低内存占用。

## [2.0.36] - 2026-05-19

### Added
- **Model Discovery**：krouter 现在会在用户连接 OpenClaw 后自动探测可用模型列表。连接时读取 OpenClaw 配置里的 Anthropic API key，调用 `/v1/models` 接口，将返回的模型列表写入 OpenClaw 的 `models.providers.anthropic.models` 字段，使 OpenClaw 的模型选择器立即呈现真实可用的模型。所有已发现模型同时缓存到本地 SQLite DB（`model_discovery` 表）。
- **OpenAI-compatible provider 模型探测**：daemon 启动 10s 后自动检查 settings 里有 key 的 provider（DeepSeek、Groq 等），若缓存超过 24h 则重新探测并更新 DB。
- **新端点 `GET /internal/models`**：返回 DB 中所有已发现模型，按 provider 分组。
- **新端点 `POST /internal/models/refresh`**：手动触发全量刷新（settings-keyed providers + 已连接的 OpenClaw Anthropic key），异步执行，立即返回 `{"ok":true}`。
- **`ModelDiscoverer` 可选接口**：Anthropic 和 OpenAI adapter 均已实现，支持通过 `keyFn` 透传 API key，不暴露给 krouter 存储层。OpenAI adapter 的 `modelsEndpointURL()` 正确处理 GLM（`/v4/models`）和 Qwen（`/compatible-mode/v1/models`）的 pathReplace。

## [2.0.35] - 2026-05-19

### Added
- **HTTP proxy 支持（OS 系统代理 + per-provider bypass）**：daemon 现在像 Chrome 一样自动读取 OS 系统代理设置（macOS `scutil --proxy`、Windows 注册表 `HKCU\Internet Settings`、Linux GNOME `gsettings`），无需用户在 krouter 里单独配置代理。国内直连 provider（MiniMax、Moonshot、DeepSeek、GLM/Zhipu、Qwen）通过内置 no-proxy 后缀列表绕过代理，Anthropic/Groq 等国际 provider 走代理。代理检测每 60s 刷新一次，VPN 接入/断开后自动生效。
- **新端点字段**：`GET /internal/status` 响应新增 `proxy` 字段（`{"url":"","source":"none","active":false}`），未来 Web UI 可在 Providers 页面展示当前代理状态。

## [2.0.34] - 2026-05-18

### Changed
- **Web UI 认证简化**：废弃 session cookie + ticket-exchange 方案，改为基于 `Origin` header 的 CSRF 防护。认证中间件决策顺序：① 有效 Bearer token → 无条件放行（CLI / 程序化访问）；② Origin 存在且不等于 `http://127.0.0.1:8403` → 403（跨域拦截，防 CSRF）；③ 无 Origin 或正确 Origin → 放行（curl / 浏览器 dashboard）。浏览器访问 dashboard 不再需要任何登录流程，daemon 重启也不影响体验。
- **删除端点**：`POST /internal/auth/ticket`、`GET /internal/auth/exchange` 已移除
- **install server 简化**：`handleDaemonReady` 不再 mint ticket，直接返回 `redirect_url: /krouter/`；移除 `readInternalTokenFn` / `mintDaemonTicketFn` 依赖

### Security
- 安全性等效：Origin header 由浏览器自动设置且不可被跨域 JS 伪造（W3C CORS 规范强制）；curl / CLI 不发 Origin，不受影响；有效 Bearer token 可绕过 CSRF 检查（持有 token 即已认证）

## [2.0.33] - 2026-05-18

### Fixed
- **Bug D — `defaultOpenClawModels` 格式错误**：之前注入的是字符串数组（如 `["claude-opus-4-5", ...]`），但 OpenClaw `ModelDefinitionSchema` 要求每个元素是对象（`{id, name, ...}`），全新机器首次运行 `krouter connect openclaw` 时 OpenClaw 会 schema validation crash。改为注入空数组 `[]`：空数组满足 `z.array(ModelDefinitionSchema)` schema、不会 crash；OpenClaw 通过 plugin 系统动态加载模型目录，不依赖该字段内容

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
