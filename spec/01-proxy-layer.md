# spec/01-proxy-layer.md — HTTP Proxy Layer

**Module**: `internal/proxy`
**Port**: 8402 (ALWAYS 127.0.0.1)
**Dependencies**: `routing`, `providers`, `logging`, `storage` (for request log)

---

## 1. Purpose

Accept LLM requests from local AI agents and forward them to upstream LLM
providers, with optional routing based on user's preset and quota state.

This is the **hot path**: every agent request goes through here. Performance,
streaming fidelity, and error handling matter.

---

## 2. Endpoints

| Method | Path | Protocol | Description |
|--------|------|----------|-------------|
| POST   | `/v1/messages` | Anthropic | Messages API (Claude Code, OpenClaw default) |
| POST   | `/v1/chat/completions` | OpenAI | Chat Completions (Cursor, OpenAI users) |
| GET    | `/v1/models` | Gateway discovery | Claude Code needs this for autocomplete |
| GET    | `/health` | Health check | 200 OK if daemon running |

All bind to `127.0.0.1:8402`. No authentication on this port — process-level
isolation is sufficient (only local user can connect).

---

## 3. Request flow

### 3.1 General pipeline

```
agent request
     ↓
[1] parse + validate
     ↓
[2] extract metadata (model, token estimate, message content type)
     ↓
[3] routing.Decide(metadata) → chosen provider+model
     ↓
[4] providers.Get(name).Forward(request)
     ↓ (streamed)
[5] copy response to client, count tokens, compute cost
     ↓
[6] write request log to SQLite (model, tokens, cost, latency)
     ↓
[7] (async) update quota state, fire notifications if thresholds hit
```

### 3.2 `/v1/messages` (Anthropic)

Wire format reference: <https://docs.anthropic.com/en/api/messages>

```
POST /v1/messages HTTP/1.1
x-api-key: sk-ant-api03-...
anthropic-version: 2023-06-01
content-type: application/json

{
  "model": "claude-sonnet-4-5",
  "messages": [...],
  "max_tokens": 4096,
  "stream": true
}
```

Handler responsibilities:

1. **Read** request body to extract `model`, `messages`, `stream`, `max_tokens`.
2. **Forward** headers including `x-api-key` UNCHANGED (this is the user's key).
3. **Route** via `routing.Decide(...)`. The result may say "use the exact model
   requested" or "downgrade to cheaper model".
4. **Rewrite** the request body if model is changed.
5. **Forward** to chosen provider's Anthropic endpoint.
6. **Stream** response back. If client requested SSE (`stream: true`), use
   `Transfer-Encoding: chunked` and flush after every event.

### 3.3 `/v1/chat/completions` (OpenAI)

Wire format reference: <https://platform.openai.com/docs/api-reference/chat>

Same as 3.2 but with OpenAI's request/response shape. Routing to an OpenAI-
protocol provider (DeepSeek, Moonshot's OpenAI endpoint, etc.).

### 3.4 SSE streaming

Both protocols support SSE. The proxy MUST:

- Set response headers: `Content-Type: text/event-stream`, `Cache-Control: no-cache`, `X-Accel-Buffering: no`
- Use `http.Flusher` to flush after each event
- Copy bytes with `io.Copy` is acceptable if combined with a periodic flush
- Use a `context.Context` that cancels when client disconnects (`r.Context()`)
- On client disconnect: cancel upstream request to stop billing

```go
flusher, ok := w.(http.Flusher)
if !ok {
    http.Error(w, "streaming not supported", http.StatusInternalServerError)
    return
}

// Forward upstream response in chunks
buf := make([]byte, 4096)
for {
    n, err := upstreamReader.Read(buf)
    if n > 0 {
        w.Write(buf[:n])
        flusher.Flush()
    }
    if err != nil {
        break // io.EOF on normal completion
    }
}
```

### 3.5 Error handling

| Upstream condition | Response to client |
|--------------------|--------------------|
| 401 Unauthorized (bad API key) | Forward 401 as-is, log "auth failed for {provider}" |
| 429 Rate limit | Forward 429 + retry-after, log "rate limited" |
| 5xx | Try fallback provider per spec/02 §5; if none, return 502 |
| Network timeout | 504 Gateway Timeout |
| Client disconnect | Cancel upstream, log "client disconnected" |

NEVER return a generic 500 — the client gets a useful error or a fallback.

---

## 4. Request log entry

After each request (success or failure), write to SQLite `requests` table:

```sql
INSERT INTO requests (
  id, ts_utc, agent, protocol, requested_model, actual_provider, actual_model,
  input_tokens, output_tokens, cached_tokens, cost_micro_usd, latency_ms,
  status_code, error_message
) VALUES (...);
```

Tokens come from the upstream response's usage block.
Cost is computed via `pricing.CostFor(provider, model, input, output)`.

NEVER log: request body content, response body content, x-api-key value.

---

## 5. Performance targets

- p50 latency overhead: < 5ms vs direct provider call
- p99 latency overhead: < 20ms
- Memory: no full-body buffering; constant memory per request
- Concurrent requests: 100+ simultaneous streaming connections without issue

---

## 6. Test coverage

- Unit: each handler with mock provider (return fixed SSE chunks, verify forwarding)
- Unit: routing decision integration (mock routing engine)
- Unit: error propagation for 401/429/5xx upstream
- Integration: full pipeline with `httptest.Server` as upstream

---

## 7. Open questions for human review

- (none currently — spec is implementation-ready)
