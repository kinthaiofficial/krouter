# spec/02-routing-engine.md — Routing Decision Engine

**Module**: `internal/routing`
**Called by**: `internal/proxy`
**Calls into**: `internal/providers`, `internal/pricing`, `internal/config`, `internal/storage`

---

## 1. Purpose

Given an incoming agent request, decide **which provider + which model** to actually use.

The same agent says "I want claude-sonnet-4-5". Depending on preset and quota:
- **Saver**: maybe route to DeepSeek (cheaper, still capable for many tasks)
- **Balanced**: route to claude-sonnet-4-5 (as requested)
- **Quality**: same, but maybe upgrade to claude-opus-4-5 for complex tasks

The decision should be **transparent** — logged with a human-readable reason
so the user can see why their request was routed somewhere.

---

## 2. Inputs

```go
type Request struct {
    Protocol       string   // "anthropic" | "openai"
    RequestedModel string   // e.g. "claude-sonnet-4-5"
    InputTokenEst  int      // rough estimate from request body size
    HasImages      bool     // multimodal flag
    HasTools       bool     // tool/function calling flag
    SystemPrompt   string   // first 200 chars for complexity classification
    AgentName      string   // "openclaw" | "claude-code" | "cursor" | "hermes" | "unknown"
    UserAPIKey     string   // for provider-availability check (DO NOT LOG)
}
```

---

## 3. Decision algorithm

```
Step 1: Determine task complexity
  - Estimate complexity score 0.0-1.0 based on:
    * input token count (higher = more complex)
    * has tools / functions
    * has images
    * system prompt keywords ("debug", "refactor", "architect" = higher)

Step 2: Check quota state (from storage)
  - 5h window: percent used
  - Weekly cap: percent used
  - Opus cap: percent used
  - If any > 90%, downgrade aggressively

Step 3: Filter eligible providers by preset
  Saver:    [cheapest providers that meet protocol requirement]
  Balanced: [requested + cheaper alternatives if complexity low]
  Quality:  [requested + upgrade options if complexity high]

Step 4: Check provider availability
  - User's env has API key for that provider? (from internal-token check)
  - Provider's recent health (last N requests, error rate)

Step 5: Score remaining providers
  - Cost weight (higher in Saver)
  - Quality weight (higher in Quality)
  - Latency weight (always nonzero)

Step 6: Pick top-scored provider
  - Return Decision{Provider, Model, Reason}
```

---

## 4. Preset definitions

### Saver

Goal: minimize cost. Accept "good enough" model.

| Task type | Saver chooses |
|-----------|---------------|
| Simple chat | deepseek-chat or claude-haiku |
| Code completion | deepseek-coder or claude-haiku |
| Complex code | claude-sonnet (no Opus) |
| Multimodal | claude-sonnet (cheapest with images) |

### Balanced

Goal: route as agent requested unless quota is tight.

| Task type | Balanced chooses |
|-----------|------------------|
| As requested | Honor the model name |
| Quota > 80% used | Downgrade one tier (sonnet → haiku) |
| Quota > 95% used | Suspend non-essential, only critical |

### Quality

Goal: best output. Upgrade if helpful.

| Task type | Quality chooses |
|-----------|-----------------|
| Complex code (>4K tokens system prompt) | claude-opus-4-5 |
| Multimodal | claude-opus (best vision) |
| Simple chat | as requested (no need to upgrade) |

---

## 5. Fallback chain

If chosen provider fails (proxy returns 5xx or timeout):

```
Decision: anthropic/claude-sonnet-4-5
  ↓ 503 from Anthropic
Try fallback list (same protocol):
  1. another Anthropic model (haiku)
  2. cross-company same-protocol provider (none for Anthropic protocol currently)
  3. return error to client
```

For OpenAI protocol there are more options (DeepSeek → Moonshot → ...).
For Anthropic protocol, fallbacks are limited.

Fallback is only on 5xx + timeout, NOT on 401/429.

---

## 6. Output

```go
type Decision struct {
    Provider string  // "anthropic" | "deepseek" | ...
    Model    string  // exact model identifier to send upstream
    Reason   string  // "Saver preset chose deepseek-chat (saved $0.04 vs requested claude-sonnet)"

    // Bookkeeping
    EstimatedCostUSD float64
    EstimatedTokens  int
}
```

The `Reason` field is shown to user in GUI logs panel and CLI `krouter logs`.
Make it human-friendly.

---

## 7. Configuration override

User can pin specific routing via `~/.kinthai/settings.json`:

```json
{
  "preset": "saver",
  "routing_overrides": {
    "openclaw": {
      "always_use": "deepseek-chat"
    }
  }
}
```

Overrides take precedence over preset. Document in GUI Settings.

---

## 8. Test coverage

- Unit: each preset against fixed inputs (table-driven tests)
- Unit: quota threshold downgrade behavior
- Unit: fallback chain on simulated 5xx
- Integration: full proxy → routing → mock provider with realistic SSE

---

## 9. Open questions for human review

- For Saver preset: should we attempt complexity classification via the LLM
  itself (a "router model")? Probably overkill for M1. Keep keyword-based.
- Provider availability detection: should we ping providers, or rely on
  request-time failures? Pinging adds work; lazy detection is simpler.
  Default: lazy.
