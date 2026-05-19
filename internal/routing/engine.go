// Package routing implements the request routing decision engine.
//
// Given an incoming agent request, the engine decides which provider and model
// to use, according to the active preset (Saver / Balanced / Quality).
//
// See spec/02-routing-engine.md for the full decision algorithm.
package routing

import (
	"context"
	"fmt"

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

// PricingSource returns per-model cost data used for tier-aware routing.
// Implementations must be safe for concurrent use.
type PricingSource interface {
	// InputCostPerToken returns the input cost in USD per single token.
	// Returns 0 for unknown models; callers treat 0 as "price unknown".
	InputCostPerToken(model string) float64
}

// SubscriptionInfo carries quota state for a subscription-based provider.
type SubscriptionInfo struct {
	Available        bool
	Model            string  // e.g. "MiniMax-M2.7" or "MiniMax-M2.7-highspeed"
	Remaining        int64   // calls left in current window
	Total            int64   // window call limit
	EffectiveCostUSD float64 // amortised per-call cost (monthly_price / monthly_calls)
}

// SubscriptionSource reports quota state for call-count-based providers (e.g. MiniMax).
// Routing prefers these providers when available; their effective per-call cost
// (~$0.000031) is lower than any per-token provider.
type SubscriptionSource interface {
	GetSubscriptionInfo(ctx context.Context, provider string) SubscriptionInfo
}

// Engine makes routing decisions.
type Engine struct {
	registry     *providers.Registry
	health       HealthChecker      // optional; nil means no health-based routing
	pricing      PricingSource      // optional; nil falls back to hardcoded model names
	subscription SubscriptionSource // optional; nil means no subscription-aware routing
}

// New creates a routing engine backed by the given provider registry.
func New(registry *providers.Registry) *Engine {
	return &Engine{registry: registry}
}

// WithHealth attaches a health checker to bias routing away from unhealthy providers.
func (e *Engine) WithHealth(h HealthChecker) {
	e.health = h
}

// WithPricing attaches a live pricing source so the engine can select the
// cheapest/most-capable model dynamically instead of using hardcoded names.
func (e *Engine) WithPricing(p PricingSource) {
	e.pricing = p
}

// WithSubscription attaches a subscription quota source for call-count-based
// providers (e.g. MiniMax). When available, these providers are preferred because
// their effective per-call cost (~$0.000031) is lower than any per-token provider.
func (e *Engine) WithSubscription(s SubscriptionSource) {
	e.subscription = s
}

// subscriptionInfo returns quota info for a provider, or zero value if not available.
// Uses background context so routing decisions are never blocked by I/O.
func (e *Engine) subscriptionInfo(provider string) SubscriptionInfo {
	if e.subscription == nil {
		return SubscriptionInfo{}
	}
	return e.subscription.GetSubscriptionInfo(context.Background(), provider)
}

// subscriptionDecision builds a Decision from SubscriptionInfo.
func subscriptionDecision(provider string, info SubscriptionInfo) Decision {
	return Decision{
		Provider:         provider,
		Model:            info.Model,
		EstimatedCostUSD: info.EffectiveCostUSD,
		Reason: fmt.Sprintf(
			"MiniMax 订阅（有效成本 $%.6f，配额剩余 %d/%d）",
			info.EffectiveCostUSD, info.Remaining, info.Total,
		),
	}
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
// When MiniMax subscription is available, it is preferred over per-token providers.
func (e *Engine) decideBalanced(req Request) Decision {
	proto := providers.Protocol(req.Protocol)

	// Prefer subscription provider when available (cost dominates for all request sizes).
	if !req.HasImages && proto == providers.ProtocolAnthropic {
		if info := e.subscriptionInfo("minimax"); info.Available {
			if _, ok := e.registry.Get("minimax"); ok {
				return subscriptionDecision("minimax", info)
			}
		}
	}

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
//   - MiniMax subscription available → MiniMax first (effective cost ~$0.000031)
//   - Anthropic protocol + no images → claude-haiku (cheapest Anthropic)
//   - OpenAI protocol → deepseek-chat (if DEEPSEEK_API_KEY set), else gpt-4o-mini fallback
//   - HasImages → claude-sonnet (cheapest Anthropic with reliable multimodal)
func (e *Engine) decideSaver(req Request) Decision {
	proto := providers.Protocol(req.Protocol)

	// Subscription provider (MiniMax) beats all per-token providers on cost.
	// Skip for image requests — MiniMax may not support multimodal reliably.
	if !req.HasImages && proto == providers.ProtocolAnthropic {
		if info := e.subscriptionInfo("minimax"); info.Available {
			if _, ok := e.registry.Get("minimax"); ok {
				return subscriptionDecision("minimax", info)
			}
		}
	}

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
		// With live pricing: pick the cheapest available (provider, model) pair.
		if prov, model := e.cheapestProviderModel(proto); prov != nil {
			return Decision{
				Provider: prov.Name(),
				Model:    model,
				Reason:   fmt.Sprintf("Saver: routing to %s via %s (live pricing)", model, prov.Name()),
			}
		}
		// Without pricing: pick the first healthy provider that explicitly lists
		// the hardcoded saver model, guarding against MiniMax contamination.
		provider := e.pickProviderForModel(proto, saverAnthropicModel)
		if provider == nil {
			return Decision{
				Provider: req.Protocol,
				Model:    req.RequestedModel,
				Reason:   fmt.Sprintf("Saver: no provider for protocol %q", req.Protocol),
			}
		}
		model := saverAnthropicModel
		if !modelSupported(provider.SupportedModels(), model) {
			model = req.RequestedModel
		}
		return Decision{
			Provider: provider.Name(),
			Model:    model,
			Reason:   fmt.Sprintf("Saver: routing to %s (cheapest Anthropic model)", saverAnthropicModel),
		}

	case providers.ProtocolOpenAI:
		// With live pricing: pick the cheapest available (provider, model) pair.
		if prov, model := e.cheapestProviderModel(proto); prov != nil {
			return Decision{
				Provider: prov.Name(),
				Model:    model,
				Reason:   fmt.Sprintf("Saver: routing to %s via %s (live pricing)", model, prov.Name()),
			}
		}
		// Without pricing: prefer DeepSeek if healthy and configured.
		if e.providerHasKey("deepseek") && e.isHealthy("deepseek") {
			if _, ok := e.registry.Get("deepseek"); ok {
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
// For simple requests (not complex), MiniMax subscription is preferred when available.
func (e *Engine) decideQuality(req Request) Decision {
	proto := providers.Protocol(req.Protocol)

	// For non-complex requests, subscription cost wins even in Quality mode.
	isComplex := req.HasImages || (req.HasTools && req.InputTokenEst > 4000)
	if !isComplex && !req.HasImages && proto == providers.ProtocolAnthropic {
		if info := e.subscriptionInfo("minimax"); info.Available {
			if _, ok := e.registry.Get("minimax"); ok {
				return subscriptionDecision("minimax", info)
			}
		}
	}

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

	// Upgrade complex tasks to the highest-capability (most expensive) model.
	isComplex = req.HasImages || (req.HasTools && req.InputTokenEst > 4000)
	if isComplex && proto == providers.ProtocolAnthropic {
		// With live pricing: pick the most expensive available model.
		if expProv, expModel := e.mostExpensiveProviderModel(proto); expProv != nil {
			return Decision{
				Provider: expProv.Name(),
				Model:    expModel,
				Reason:   fmt.Sprintf("Quality: upgrading complex request to %s via %s (live pricing)", expModel, expProv.Name()),
			}
		}
		// Without pricing: fall back to hardcoded Opus.
		model = "claude-opus-4-5"
		reason = "Quality: upgrading complex request to claude-opus-4-5"
	} else if !modelSupported(provider.SupportedModels(), model) {
		model = fallbackModel
		reason = fmt.Sprintf("Quality: requested model %q not recognised, using %s", req.RequestedModel, model)
	}

	return Decision{Provider: provider.Name(), Model: model, Reason: reason}
}

// cheapestProviderModel returns the (provider, model) pair with the lowest
// InputCostPerToken for the given protocol. Only healthy providers with a
// configured key are considered. Returns nil, "" if pricing is unavailable
// or no priced model is found.
func (e *Engine) cheapestProviderModel(proto providers.Protocol) (providers.Provider, string) {
	if e.pricing == nil {
		return nil, ""
	}
	var bestProv providers.Provider
	var bestModel string
	var bestCost float64 = -1
	for _, p := range e.registry.All() {
		if p.Protocol() != proto || !e.isHealthy(p.Name()) || !e.providerHasKey(p.Name()) {
			continue
		}
		for _, m := range p.SupportedModels() {
			c := e.pricing.InputCostPerToken(m)
			if c > 0 && (bestCost < 0 || c < bestCost) {
				bestCost = c
				bestProv = p
				bestModel = m
			}
		}
	}
	return bestProv, bestModel
}

// mostExpensiveProviderModel returns the (provider, model) pair with the highest
// InputCostPerToken for the given protocol. Only healthy providers with a
// configured key are considered. Returns nil, "" if pricing is unavailable
// or no priced model is found.
func (e *Engine) mostExpensiveProviderModel(proto providers.Protocol) (providers.Provider, string) {
	if e.pricing == nil {
		return nil, ""
	}
	var bestProv providers.Provider
	var bestModel string
	var bestCost float64
	for _, p := range e.registry.All() {
		if p.Protocol() != proto || !e.isHealthy(p.Name()) || !e.providerHasKey(p.Name()) {
			continue
		}
		for _, m := range p.SupportedModels() {
			c := e.pricing.InputCostPerToken(m)
			if c > bestCost {
				bestCost = c
				bestProv = p
				bestModel = m
			}
		}
	}
	return bestProv, bestModel
}

// providerHasKey reports whether the named provider currently has an API key
// available (via settings or environment variable). Providers that implement
// providers.Configurable are queried directly; others are assumed to have a key.
func (e *Engine) providerHasKey(name string) bool {
	p, ok := e.registry.Get(name)
	if !ok {
		return false
	}
	if c, ok := p.(providers.Configurable); ok {
		return c.HasKey()
	}
	return true
}

func modelSupported(supported []string, model string) bool {
	for _, m := range supported {
		if m == model {
			return true
		}
	}
	return false
}
