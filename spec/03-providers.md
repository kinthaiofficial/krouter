# spec/03-providers.md — Provider Adapters

**Module**: `internal/providers`
**Used by**: `internal/routing`, `internal/proxy`, `internal/pricing`

---

## 1. Provider model

A **provider** is a `(company, protocol)` tuple.

```
anthropic     = Anthropic, Anthropic protocol
openai        = OpenAI, OpenAI protocol
deepseek      = DeepSeek, OpenAI protocol (DeepSeek's compat endpoint)
moonshot-cn   = Moonshot CN, OpenAI protocol
moonshot-int  = Moonshot international, Anthropic protocol
groq          = Groq, OpenAI protocol
glm           = Zhipu/智谱, OpenAI protocol (path: /v4)
qwen          = Aliyun, OpenAI protocol (path: /compatible-mode/v1)
```

Same company with different protocols = separate providers.

---

## 2. Adapter classes

Don't write one adapter per provider. Use 3 generic adapters with config:

### OpenAIAdapter (most providers)

Generic adapter that speaks OpenAI Chat Completions wire format. Configurable:
- BaseURL (e.g. `https://api.deepseek.com/v1`)
- AuthHeader (`Authorization: Bearer <key>` for most)
- Path override (e.g. `/compatible-mode/v1` for Qwen)
- Model name mapping (some providers rename: `qwen-plus` vs `qwen-max`)

Covers: openai, deepseek, moonshot-cn, groq, glm, qwen, mistral, fireworks,
together, perplexity, ...

### AnthropicAdapter

Speaks Anthropic Messages API. Configurable:
- BaseURL (`https://api.anthropic.com` default)
- AuthHeader (`x-api-key: <key>`)

Covers: anthropic, moonshot-int (when they expose Anthropic protocol).

### GeminiDevAdapter (M4+)

Google Gemini Developer API.

---

## 3. Interface

```go
type Provider interface {
    Name() string                        // "anthropic", "deepseek", ...
    Protocol() Protocol                   // anthropic|openai|gemini
    SupportedModels() []string            // ["claude-sonnet-4-5", ...]
    Forward(ctx, *http.Request) (io.ReadCloser, http.Header, error)
}
```

`Forward` rewrites the request URL/host to the provider's endpoint, sends it,
and returns the streaming response body. Headers are forwarded for the proxy
to relay.

---

## 4. M1 providers (must work day-one)

| Provider | Protocol | Adapter | Base URL |
|----------|----------|---------|----------|
| anthropic | anthropic | AnthropicAdapter | `https://api.anthropic.com` |
| openai | openai | OpenAIAdapter | `https://api.openai.com/v1` |
| deepseek | openai | OpenAIAdapter | `https://api.deepseek.com/v1` |

This minimal set lets us validate end-to-end M1.

## 5. M2-M3 providers (added incrementally)

| Provider | Protocol | Notes |
|----------|----------|-------|
| moonshot-cn | openai | China users |
| groq | openai | Fastest inference |
| glm | openai | `/v4` path quirk |
| qwen | openai | `/compatible-mode/v1` path |

## 6. M4+ providers

| Provider | Notes |
|----------|-------|
| moonshot-int | Anthropic protocol option |
| mistral | OpenAI protocol |
| fireworks | OpenAI protocol |
| together | OpenAI protocol |
| gemini | Needs GeminiDevAdapter |
| bedrock | AWS, complex auth — evaluate |

---

## 7. Detection: which providers does user have?

User configures providers by setting env vars in their shell (or agent config):

```
ANTHROPIC_API_KEY=sk-ant-...
DEEPSEEK_API_KEY=sk-deepseek-...
GROQ_API_KEY=gsk_...
```

Detection logic (run on daemon startup, refresh on settings change):

```go
for each known provider:
    if env var is set in current shell:
        provider is "available"
    else:
        provider is "unavailable" (greyed out in GUI)
```

Note: daemon was launched by LaunchAgent etc., which inherits the user's login
shell env. So `os.Getenv("ANTHROPIC_API_KEY")` works for detection.

DO NOT store the actual key value. Just remember "anthropic is available".
Forward the env var as the auth header at request time.

---

## 8. Health tracking

Per-provider rolling stats:
- Last 100 requests: success rate
- Last 100 requests: avg latency
- Last error code

Used by routing engine to deprioritize flaky providers. NOT used to permanently
exclude a provider (network glitches happen).

---

## 9. Test coverage

- Unit: each adapter against mock HTTP server
- Unit: model name mapping correctness
- Unit: env-var detection logic
- Integration: real call to provider with test key (CI optional)

---

## 10. Open questions

- (none currently)
