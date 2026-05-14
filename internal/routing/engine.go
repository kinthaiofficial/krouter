// Package routing implements the request routing decision engine.
//
// Given an incoming agent request, the engine decides which provider and model
// to use, according to the active preset (Saver / Balanced / Quality).
//
// See spec/02-routing-engine.md for the full decision algorithm.
package routing

import (
	"fmt"
	"os"

	"github.com/kinthaiofficial/krouter/internal/providers"
)

// Preset constants match the values stored in settings_kv.
const (
	PresetSaver    = "saver"
	PresetBalanced = "balanced"
	PresetQuality  = "quality"
)

// saverAnthropicModel is the cheapest Anthropic model used by the Saver preset.
const saverAnthropicModel = "claude-haiku-4-5-20251001"

// saverOpenAIModel is the cheapest OpenAI-protocol model used by the Saver preset.
const saverOpenAIModel = "deepseek-chat"

// fallbackModel is used when the requested model is not in SupportedModels under Balanced preset.
const fallbackModel = "claude-haiku-4-5-20251001"

// Request is the routing engine input, derived from the incoming agent request.
type Request struct {
	Protocol       string // "anthropic" | "openai"
	RequestedModel string // e.g. "claude-sonnet-4-5"
	InputTokenEst  int    // rough estimate: body bytes / 4
	HasImages      bool
	HasTools       bool
	AgentName      string // "claude-code" | "openclaw" | "cursor" | "unknown"
	UserAPIKey     string // forwarded at request time — DO NOT LOG
}

// Decision is the routing engine output.
type Decision struct {
	Provider         string
	Model            string
	Reason           string
	EstimatedCostUSD float64
	EstimatedTokens  int
}

// HealthChecker provides provider health metrics used for routing decisions.
type HealthChecker interface {
	ConsecutiveFailures(provider string) int
}

// Engine makes routing decisions.
type Engine struct {
	registry *providers.Registry
	health   HealthChecker // optional; nil means no health-based routing
}

// New creates a routing engine backed by the given provider registry.
func New(registry *providers.Registry) *Engine {
	return &Engine{registry: registry}
}

// WithHealth attaches a health checker to bias routing away from unhealthy providers.
func (e *Engine) WithHealth(h HealthChecker) {
	e.health = h
}

// isHealthy returns false if the provider has ≥3 consecutive failures.
func (e *Engine) isHealthy(providerName string) bool {
	if e.health == nil {
		return true
	}
	return e.health.ConsecutiveFailures(providerName) < 3
}

// pickHealthyProvider returns the first healthy provider for the given protocol,
// falling back to any provider if all are unhealthy.
func (e *Engine) pickHealthyProvider(proto providers.Protocol) providers.Provider {
	all := e.registry.All()
	var fallback providers.Provider
	for _, p := range all {
		if p.Protocol() != proto {
			continue
		}
		if fallback == nil {
			fallback = p
		}
		if e.isHealthy(p.Name()) {
			return p
		}
	}
	return fallback // nil if no provider for this protocol
}

// pickProviderForModel returns the first healthy provider that explicitly supports
// the requested model. Falls back to pickHealthyProvider if none match.
func (e *Engine) pickProviderForModel(proto providers.Protocol, model string) providers.Provider {
	all := e.registry.All()
	for _, p := range all {
		if p.Protocol() != proto {
			continue
		}
		if modelSupported(p.SupportedModels(), model) && e.isHealthy(p.Name()) {
			return p
		}
	}
	return e.pickHealthyProvider(proto)
}

// Decide returns the routing decision for the given request and preset.
// preset must be one of "saver", "balanced", "quality" (case-sensitive).
// An empty or unrecognised preset is treated as "balanced".
func (e *Engine) Decide(req Request, preset string) Decision {
	switch preset {
	case PresetSaver:
		return e.decideSaver(req)
	case PresetQuality:
		return e.decideQuality(req)
	default:
		return e.decideBalanced(req)
	}
}

// decideBalanced honours the requested model; prefers the provider that explicitly
// supports it, then falls back to fallbackModel on the default provider.
func (e *Engine) decideBalanced(req Request) Decision {
	proto := providers.Protocol(req.Protocol)

	// Prefer a provider that explicitly lists the requested model.
	provider := e.pickProviderForModel(proto, req.RequestedModel)
	if provider == nil {
		return Decision{
			Provider: req.Protocol,
			Model:    req.RequestedModel,
			Reason:   fmt.Sprintf("no provider registered for protocol %q", req.Protocol),
		}
	}

	model := req.RequestedModel
	reason := fmt.Sprintf("Balanced: honoring requested model %s via %s", model, provider.Name())

	if !modelSupported(provider.SupportedModels(), model) {
		model = fallbackModel
		reason = fmt.Sprintf("Balanced: requested model %q not recognised, using %s", req.RequestedModel, model)
	}

	return Decision{Provider: provider.Name(), Model: model, Reason: reason}
}

// decideSaver routes to the cheapest available provider.
//
// Rules (spec/02-routing-engine.md §4):
//   - Anthropic protocol + no images → claude-haiku (cheapest Anthropic)
//   - OpenAI protocol → deepseek-chat (if DEEPSEEK_API_KEY set), else gpt-4o-mini fallback
//   - HasImages → claude-sonnet (cheapest Anthropic with reliable multimodal)
func (e *Engine) decideSaver(req Request) Decision {
	proto := providers.Protocol(req.Protocol)

	// Multimodal requires a capable model regardless of preset.
	if req.HasImages {
		provider := e.pickHealthyProvider(proto)
		if provider == nil {
			return Decision{
				Provider: req.Protocol,
				Model:    req.RequestedModel,
				Reason:   fmt.Sprintf("Saver: no provider for protocol %q", req.Protocol),
			}
		}
		return Decision{
			Provider: provider.Name(),
			Model:    "claude-sonnet-4-5",
			Reason:   "Saver: multimodal request requires vision-capable model",
		}
	}

	switch proto {
	case providers.ProtocolAnthropic:
		provider := e.pickHealthyProvider(proto)
		if provider == nil {
			return Decision{
				Provider: req.Protocol,
				Model:    req.RequestedModel,
				Reason:   fmt.Sprintf("Saver: no provider for protocol %q", req.Protocol),
			}
		}
		return Decision{
			Provider: provider.Name(),
			Model:    saverAnthropicModel,
			Reason:   fmt.Sprintf("Saver: routing to %s (cheapest Anthropic model)", saverAnthropicModel),
		}

	case providers.ProtocolOpenAI:
		// Prefer DeepSeek if healthy and available (env key present and registered).
		if isProviderAvailable("deepseek") && e.isHealthy("deepseek") {
			if dp, ok := e.registry.Get("deepseek"); ok {
				_ = dp
				return Decision{
					Provider: "deepseek",
					Model:    saverOpenAIModel,
					Reason:   fmt.Sprintf("Saver: routing to %s (cheapest OpenAI-compatible model)", saverOpenAIModel),
				}
			}
		}
		// Fall back to whatever healthy OpenAI-protocol provider is registered.
		provider := e.pickHealthyProvider(proto)
		if provider == nil {
			return Decision{
				Provider: req.Protocol,
				Model:    req.RequestedModel,
				Reason:   "Saver: no OpenAI-protocol provider available",
			}
		}
		return Decision{
			Provider: provider.Name(),
			Model:    req.RequestedModel,
			Reason:   "Saver: using registered OpenAI-protocol provider",
		}

	default:
		return e.decideBalanced(req)
	}
}

// decideQuality upgrades complex requests; otherwise honours the request.
func (e *Engine) decideQuality(req Request) Decision {
	proto := providers.Protocol(req.Protocol)

	provider := e.pickProviderForModel(proto, req.RequestedModel)
	if provider == nil {
		return Decision{
			Provider: req.Protocol,
			Model:    req.RequestedModel,
			Reason:   fmt.Sprintf("Quality: no provider for protocol %q", req.Protocol),
		}
	}

	model := req.RequestedModel
	reason := fmt.Sprintf("Quality: honoring requested model %s via %s", model, provider.Name())

	// Upgrade to Opus for complex tasks (images, many tools, large input).
	isComplex := req.HasImages || (req.HasTools && req.InputTokenEst > 4000)
	if isComplex && proto == providers.ProtocolAnthropic {
		model = "claude-opus-4-5"
		reason = "Quality: upgrading complex request to claude-opus-4-5"
	} else if !modelSupported(provider.SupportedModels(), model) {
		model = fallbackModel
		reason = fmt.Sprintf("Quality: requested model %q not recognised, using %s", req.RequestedModel, model)
	}

	return Decision{Provider: provider.Name(), Model: model, Reason: reason}
}

// isProviderAvailable returns true if the provider's API key env var is set.
func isProviderAvailable(providerName string) bool {
	envVars := map[string]string{
		"anthropic": "ANTHROPIC_API_KEY",
		"deepseek":  "DEEPSEEK_API_KEY",
		"openai":    "OPENAI_API_KEY",
		"groq":      "GROQ_API_KEY",
		"moonshot":  "MOONSHOT_API_KEY",
	}
	env, ok := envVars[providerName]
	if !ok {
		return false
	}
	return os.Getenv(env) != ""
}

func modelSupported(supported []string, model string) bool {
	for _, m := range supported {
		if m == model {
			return true
		}
	}
	return false
}
