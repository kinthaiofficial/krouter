# spec/04-pricing.md — Pricing Service

**Module**: `internal/pricing`
**Used by**: `internal/routing` (cost-based decisions), `internal/proxy` (per-request cost log)

---

## 1. Purpose

Maintain an up-to-date table of LLM pricing (input/output cost per million
tokens) for all supported providers/models.

Without accurate pricing, our "省了多少钱" claims are marketing fluff.
Pricing accuracy is the foundation of differentiation.

---

## 2. Three-layer architecture

```
Layer 1: Static fallback (bundled at build time)
  └── model_prices_v1.json embedded in binary
      Used when:
        - Daemon first run (no sync yet)
        - All upstream sync sources fail
      Updated:
        - Each release tags includes a fresh snapshot

Layer 2: Live sync (every 24h)
  └── Fetch LiteLLM model_prices_and_context_window.json
      from GitHub raw URL
      Verify SHA-256 against expected hash list
      Replace in-memory table on success
      Cache in SQLite for offline use

Layer 3: Per-request accounting (always)
  └── Each request: tokens_used × price_per_token = cost
      Cached read tokens get discount per Anthropic doc
      Write to requests table
```

---

## 3. LiteLLM JSON sync

Source: `https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json`

Schema (simplified):
```json
{
  "claude-sonnet-4-5": {
    "max_tokens": 200000,
    "input_cost_per_token": 0.000003,
    "output_cost_per_token": 0.000015,
    "cache_read_input_token_cost": 0.00000030,
    "cache_creation_input_token_cost": 0.00000375,
    "provider": "anthropic"
  },
  "deepseek-chat": {
    "input_cost_per_token": 0.00000014,
    "output_cost_per_token": 0.00000028,
    "provider": "deepseek"
  }
}
```

Sync logic:
1. HTTP GET with If-Modified-Since header
2. Parse JSON, validate top-level model count > 50 (sanity check)
3. Compute SHA-256 of body
4. Replace in-memory map atomically (RWMutex)
5. Update SQLite cache + last_synced timestamp
6. On parse error or partial data: keep last good copy, log warning

Failure mode: never delete cached data. Falling back to "stale prices" is
better than "no prices".

---

## 4. Cost computation

```go
func CostFor(provider, model string, inputTokens, outputTokens, cachedTokens int) int64 {
    entry := lookup(provider, model)
    if entry == nil {
        return 0  // unknown model: log warning, return 0 (don't error)
    }
    
    // Cached tokens have discount (Anthropic: 10% of input cost)
    regularInput := inputTokens - cachedTokens
    
    cost := float64(regularInput) * entry.InputCostPerToken +
            float64(cachedTokens) * entry.CachedInputCostPerToken +
            float64(outputTokens) * entry.OutputCostPerToken
    
    return int64(cost * 1_000_000)  // micro-USD for precision
}
```

Return micro-USD (1e6 = $1) for precise accumulation.
Display layer divides by 1e6 for $X.XX format.

---

## 5. Price tiers for preset

The routing engine uses pricing to map models into tiers:

```
Tier 0 (free / nearly free): groq llama, deepseek-coder, very cheap qwen models
Tier 1 (cheap):              claude-haiku, gpt-4o-mini, deepseek-chat
Tier 2 (mid):                claude-sonnet, gpt-4o, qwen-plus
Tier 3 (expensive):          claude-opus, gpt-4-turbo
```

Tier mapping is computed at startup from current pricing data.
Saver picks Tier 0-1. Quality picks Tier 2-3. Balanced honors request.

---

## 6. SQLite schema

```sql
CREATE TABLE pricing_cache (
    model_id            TEXT PRIMARY KEY,         -- "claude-sonnet-4-5"
    provider            TEXT NOT NULL,
    input_cost_per_token         REAL,
    output_cost_per_token        REAL,
    cached_input_cost_per_token  REAL,
    max_tokens                   INTEGER,
    raw_json            TEXT,                     -- original LiteLLM entry for debugging
    updated_at          TIMESTAMP NOT NULL
);

CREATE TABLE pricing_sync_meta (
    key   TEXT PRIMARY KEY,
    value TEXT
);
-- Records: last_sync_at, last_etag, last_sha256, source_url
```

---

## 7. Provider Adapter and pricing alignment

Crucial: routing engine knows model `claude-sonnet-4-5` belongs to provider
`anthropic`. The proxy forwards to `https://api.anthropic.com`. Pricing entry
must use the same model name as the upstream API.

Different agents may name models differently. Normalize:
- "claude-sonnet-4-5" → `claude-sonnet-4-5`
- "claude-sonnet-4-5-20250101" → `claude-sonnet-4-5` (strip date suffix)
- "claude-sonnet@latest" → `claude-sonnet-4-5` (alias resolution via providers/registry)

---

## 8. GUI visibility

Settings → Pricing shows:
- Last sync time
- Source (LiteLLM JSON / fallback / SQLite cache)
- Top 10 models by usage with current price
- Total saved this month ($X.XX vs always-using-claude-opus baseline)

---

## 9. Test coverage

- Unit: JSON parsing of LiteLLM schema
- Unit: SHA-256 verification
- Unit: CostFor with cached tokens
- Unit: Tier mapping computation
- Integration: HTTP fetch with mock server, ETag handling

---

## 10. Open questions

- What if LiteLLM removes a model we use? Probably keep cached entry until
  it's clearly outdated (last_seen + 30 days). Spec: keep forever, mark as
  "deprecated" in UI.
