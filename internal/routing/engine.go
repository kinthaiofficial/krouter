// Package routing implements the request routing decision engine.
//
// Given an incoming agent request, the engine decides which provider and model
// to use, according to the active preset (Saver / Balanced / Quality).
//
// See spec/02-routing-engine.md for the full decision algorithm.
package routing

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/kinthaiofficial/krouter/internal/providers"
)

// ErrBudgetExceeded is returned by the routing engine when the daily (or
// weekly) spend limit has been reached. The proxy layer turns this into an
// HTTP 429 so the agent sees a clear, actionable error instead of a 502.
var ErrBudgetExceeded = errors.New("daily budget limit exceeded")

// complexityKeywords indicate a request that likely benefits from a more capable model.
var complexityKeywords = []string{
	"debug", "refactor", "architect", "design", "analyze",
	"optimize", "implement", "review", "audit", "migration",
}

// ComplexityScore returns a score in [0.0, 1.0] estimating request complexity.
// Scores >= 0.4 are treated as "complex" by the Quality preset.
// Exported for testing; internal callers use the same function.
func ComplexityScore(req Request) float64 {
	score := 0.0

	if req.HasImages {
		score += 0.4
	}
	if req.InputTokenEst > 10000 {
		score += 0.4
	} else if req.InputTokenEst > 4000 {
		score += 0.2
	}
	if req.HasTools && req.InputTokenEst > 4000 {
		score += 0.15
	}

	sp := strings.ToLower(req.SystemPrompt)
	for _, kw := range complexityKeywords {
		if strings.Contains(sp, kw) {
			score += 0.05
		}
	}

	if score > 1.0 {
		return 1.0
	}
	return score
}

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

// fallbackModelFor returns the per-protocol model name to use when the
// requested model isn't in the chosen provider's SupportedModels list.
//
// Previously a single anthropic-only constant (`claude-haiku-4-5-20251001`)
// was used for both protocols, so an OpenAI-protocol request with an
// unknown model (e.g. `baidu/cobuddy:free`) ended up sent to an OpenAI
// provider like mistral or groq carrying an Anthropic model name —
// the upstream rejected with HTTP 401 / 400. We now pick a protocol-
// appropriate cheap default (saverOpenAIModel / saverAnthropicModel)
// so the upstream at least has a fighting chance of recognising the
// model id.
func fallbackModelFor(proto providers.Protocol) string {
	if proto == providers.ProtocolAnthropic {
		return saverAnthropicModel
	}
	return saverOpenAIModel
}

// Request is the routing engine input, derived from the incoming agent request.
type Request struct {
	Protocol       string // "anthropic" | "openai"
	RequestedModel string // e.g. "claude-sonnet-4-5"
	InputTokenEst  int    // rough estimate: body bytes / 4
	HasImages      bool
	HasTools       bool
	SystemPrompt   string // first 300 chars of system prompt, for complexity classification
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
	// BudgetExceeded is true when the daily/weekly spend limit has been hit.
	// The proxy returns HTTP 429 and does not forward the request upstream.
	BudgetExceeded bool
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

// QuotaState describes current budget consumption percentages (0.0–1.0).
// A value of 0 means "no budget configured" — no downgrade is triggered.
type QuotaState struct {
	DailyPercent  float64 // today's cost / daily budget limit
	WeeklyPercent float64 // this week's cost / weekly budget limit
	OpusPercent   float64 // 24h Opus tokens / soft cap (500K tokens)
}

// QuotaSource provides the current quota consumption percentages.
// Implementations must be safe for concurrent use.
type QuotaSource interface {
	CurrentQuota(ctx context.Context) QuotaState
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

// OverrideSource provides per-agent routing overrides configured by the user.
// An empty alwaysUse and preset mean "no override for this agent".
type OverrideSource interface {
	GetRoutingOverride(agentName string) (alwaysUse, preset string)
}

// FreeProviderSource identifies providers the user has configured in an
// agent (via inherited_endpoints) AND that the curated data/free_tokens.json
// lists as offering free credits / quotas. Routing treats these as
// "should be preferred over paid candidates" without the user doing
// anything beyond pasting the API key into their agent — the catalog
// match is what tells krouter the key is from a free credit.
//
// Implementations must also account for the 4xx exhaustion mark
// (provider_exhausted_until table): a provider currently exhausted is
// NOT returned, so routing automatically falls back to paid candidates
// until the exhaustion TTL expires.
type FreeProviderSource interface {
	// ListAvailableFreeProviders returns the krouter_provider_names of
	// free-credit providers the user has configured in some agent AND
	// which are not currently marked exhausted. Filtered to providers
	// whose adapter speaks the given protocol; routing iterates this
	// list before consulting the paid-pricing cheapest path.
	ListAvailableFreeProviders(ctx context.Context, protocol string) []string
}

// Engine makes routing decisions.
type Engine struct {
	registry      *providers.Registry
	health        HealthChecker       // optional; nil means no health-based routing
	pricing       PricingSource       // optional; nil falls back to hardcoded model names
	subscription  SubscriptionSource  // optional; nil means no subscription-aware routing
	quota         QuotaSource         // optional; nil means no quota-based downgrade
	overrides     OverrideSource      // optional; nil means no per-agent overrides
	freeProviders FreeProviderSource  // optional; nil means free-first routing disabled
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

// WithQuota attaches a quota source for Anthropic token budget downgrade logic.
func (e *Engine) WithQuota(q QuotaSource) {
	e.quota = q
}

// WithOverrides attaches a per-agent routing override source.
func (e *Engine) WithOverrides(o OverrideSource) {
	e.overrides = o
}

// WithFreeProviders attaches the free-credit provider source. When set,
// routing prefers providers the user has inherited from their agents
// that match the curated free-token catalog (data/free_tokens.json),
// falling back to the paid-pricing cheapest path on either no-match or
// the provider being marked exhausted from a recent 4xx.
func (e *Engine) WithFreeProviders(f FreeProviderSource) {
	e.freeProviders = f
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
	// Hard stop: block the request when the daily or weekly budget is exhausted.
	// Per-agent overrides do NOT bypass this — budget is an absolute ceiling.
	if e.quota != nil {
		qs := e.quota.CurrentQuota(context.Background())
		if qs.DailyPercent >= 1.0 || qs.WeeklyPercent >= 1.0 {
			return Decision{BudgetExceeded: true, Reason: ErrBudgetExceeded.Error()}
		}
	}

	// Per-agent override takes priority over preset and quota logic.
	if e.overrides != nil && req.AgentName != "" {
		if alwaysUse, overridePreset := e.overrides.GetRoutingOverride(req.AgentName); alwaysUse != "" {
			proto := providers.Protocol(req.Protocol)
			provider := e.pickProviderForModel(proto, alwaysUse)
			if provider != nil {
				dec := Decision{
					Provider: provider.Name(),
					Model:    alwaysUse,
					Reason:   fmt.Sprintf("per-agent override for %s: always_use %s", req.AgentName, alwaysUse),
				}
				e.enrichDecision(&dec, req)
				return dec
			}
		} else if overridePreset != "" {
			preset = overridePreset
		}
	}

	preset = e.applyQuotaDowngrade(preset)
	var dec Decision
	switch preset {
	case PresetSaver:
		dec = e.decideSaver(req)
	case PresetQuality:
		dec = e.decideQuality(req)
	default:
		dec = e.decideBalanced(req)
	}
	e.enrichDecision(&dec, req)
	return dec
}

// FallbackDecide returns the next routing decision after excluding already-tried
// provider/model pairs. It is called by the proxy layer on 5xx or timeout errors.
// Returns a zero Decision (Provider == "") when no further fallback is available.
// 401 and 429 responses must NOT trigger fallback — the caller is responsible for
// checking the status code before calling this method.
func (e *Engine) FallbackDecide(req Request, preset string, tried map[string]bool) Decision {
	proto := providers.Protocol(req.Protocol)
	if proto == providers.ProtocolAnthropic {
		return e.fallbackAnthropic(req, tried)
	}
	return e.fallbackOpenAI(req, tried)
}

// fallbackAnthropic returns the next lower Anthropic model tier: opus→sonnet→haiku.
// It scans from highest to lowest tier, skipping any tier already in tried.
func (e *Engine) fallbackAnthropic(_ Request, tried map[string]bool) Decision {
	type tier struct {
		matchSubstr   string
		fallbackModel string
		label         string
	}
	// Ordered from highest to lowest capability.
	tiers := []tier{
		{"opus", "claude-sonnet-4-6", "sonnet"},
		{"sonnet", "claude-haiku-4-5-20251001", "haiku"},
	}

	if _, ok := e.registry.Get("anthropic"); !ok || !e.isHealthy("anthropic") {
		return Decision{}
	}

	for _, t := range tiers {
		// Is any tried model at this tier?
		var srcModel string
		for k := range tried {
			if parts := strings.SplitN(k, "/", 2); len(parts) == 2 {
				if strings.Contains(strings.ToLower(parts[1]), t.matchSubstr) {
					srcModel = parts[1]
					break
				}
			}
		}
		if srcModel == "" {
			continue // this tier was not tried yet
		}
		// Tier was tried — offer the fallback if not already tried.
		fbKey := "anthropic/" + t.fallbackModel
		if tried[fbKey] {
			continue // fallback also exhausted; check next tier
		}
		return Decision{
			Provider: "anthropic",
			Model:    t.fallbackModel,
			Reason:   fmt.Sprintf("5xx fallback: %s → %s", srcModel, t.label),
		}
	}
	return Decision{}
}

// fallbackOpenAI returns the next healthy same-protocol provider not already tried.
func (e *Engine) fallbackOpenAI(req Request, tried map[string]bool) Decision {
	proto := providers.Protocol(req.Protocol)
	for _, p := range e.registry.All() {
		if p.Protocol() != proto || !e.isHealthy(p.Name()) {
			continue
		}
		model := req.RequestedModel
		if len(p.SupportedModels()) > 0 && !modelSupported(p.SupportedModels(), model) {
			model = p.SupportedModels()[0]
		}
		key := p.Name() + "/" + model
		if tried[key] {
			continue
		}
		return Decision{
			Provider: p.Name(),
			Model:    model,
			Reason:   fmt.Sprintf("5xx fallback: switching to %s/%s", p.Name(), model),
		}
	}
	return Decision{}
}

// enrichDecision fills EstimatedCostUSD and appends a savings note to Reason
// when the routing decision is cheaper than the originally requested model.
func (e *Engine) enrichDecision(dec *Decision, req Request) {
	if e.pricing == nil || dec.Provider == "" || req.InputTokenEst == 0 {
		return
	}

	// Fill EstimatedCostUSD if not already set (subscription decisions set it themselves).
	if dec.EstimatedCostUSD == 0 && dec.Model != "" {
		costPerToken := e.pricing.InputCostPerToken(dec.Model)
		dec.EstimatedCostUSD = costPerToken * float64(req.InputTokenEst)
	}

	// Append savings note when routing to a cheaper model than what was requested.
	if dec.Model == req.RequestedModel || req.RequestedModel == "" {
		return
	}
	requestedCost := e.pricing.InputCostPerToken(req.RequestedModel) * float64(req.InputTokenEst)
	routedCost := e.pricing.InputCostPerToken(dec.Model) * float64(req.InputTokenEst)
	if requestedCost > 0 && routedCost < requestedCost {
		savings := (requestedCost - routedCost) / requestedCost * 100
		dec.Reason = fmt.Sprintf("%s（比 %s 便宜 %.0f%%）", dec.Reason, req.RequestedModel, savings)
	}
}

// applyQuotaDowngrade returns a (potentially downgraded) preset based on current
// Anthropic quota consumption. Rules (spec/02-routing-engine.md §3 Step 2):
//
//	DailyPercent or WeeklyPercent >= 0.95 → force "saver" (cheapest model)
//	DailyPercent or WeeklyPercent >= 0.80 → downgrade balanced/quality by one tier
//	OpusPercent >= 0.90                   → block Opus (handled in decideQuality)
func (e *Engine) applyQuotaDowngrade(preset string) string {
	if e.quota == nil {
		return preset
	}
	qs := e.quota.CurrentQuota(context.Background())

	if qs.DailyPercent >= 0.95 || qs.WeeklyPercent >= 0.95 {
		return PresetSaver
	}
	if qs.DailyPercent >= 0.80 || qs.WeeklyPercent >= 0.80 {
		if preset == PresetQuality {
			return PresetBalanced
		}
		if preset == PresetBalanced {
			return PresetSaver
		}
	}
	return preset
}

// isOpusBlocked returns true when the Opus 24h token cap (90%) has been reached.
func (e *Engine) isOpusBlocked() bool {
	if e.quota == nil {
		return false
	}
	return e.quota.CurrentQuota(context.Background()).OpusPercent >= 0.90
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
		model = fallbackModelFor(proto)
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
// Complexity is determined by complexityScore >= 0.4.
// For simple requests, MiniMax subscription is preferred when available.
func (e *Engine) decideQuality(req Request) Decision {
	proto := providers.Protocol(req.Protocol)
	isComplex := ComplexityScore(req) >= 0.4

	// For non-complex requests without images, subscription cost wins even in Quality mode.
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

	// Upgrade complex tasks to the highest-capability (most expensive) model,
	// unless the Opus 24h cap has been reached.
	opusBlocked := e.isOpusBlocked()
	if isComplex && proto == providers.ProtocolAnthropic && !opusBlocked {
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
	} else if isComplex && opusBlocked {
		model = "claude-sonnet-4-6"
		reason = "Quality: Opus 24h 用量已达上限，降级到 sonnet"
	} else if !modelSupported(provider.SupportedModels(), model) {
		model = fallbackModelFor(proto)
		reason = fmt.Sprintf("Quality: requested model %q not recognised, using %s", req.RequestedModel, model)
	}

	return Decision{Provider: provider.Name(), Model: model, Reason: reason}
}

// cheapestProviderModel returns the (provider, model) pair with the lowest
// InputCostPerToken for the given protocol. Only healthy providers with a
// configured key are considered. Returns nil, "" if pricing is unavailable
// or no priced model is found.
//
// Free-credit providers are tried first when a FreeProviderSource is
// attached: any inherited provider that the data/free_tokens.json
// catalog lists as offering free credits and that isn't currently marked
// exhausted will be preferred over paid candidates. This is invisible to
// the caller — same return shape, same downstream Decision flow.
func (e *Engine) cheapestProviderModel(proto providers.Protocol) (providers.Provider, string) {
	if e.pricing == nil {
		return nil, ""
	}
	// Free-first: when a free-credit provider is configured and not
	// exhausted, route to it (cheapest model on that provider). We
	// don't compare effective_cost against paid candidates because any
	// free credit beats any paid token price.
	if prov, model := e.cheapestFreeProviderModel(proto); prov != nil {
		return prov, model
	}
	return e.cheapestPaidProviderModel(proto)
}

// cheapestFreeProviderModel returns the cheapest (provider, model) pair
// among free-credit providers the user has actually configured. Returns
// nil, "" when the FreeProviderSource is unattached, the list is empty,
// or no available free provider can serve `proto`.
func (e *Engine) cheapestFreeProviderModel(proto providers.Protocol) (providers.Provider, string) {
	if e.freeProviders == nil {
		return nil, ""
	}
	names := e.freeProviders.ListAvailableFreeProviders(context.Background(), string(proto))
	if len(names) == 0 {
		return nil, ""
	}
	// Index for O(1) membership tests inside the registry loop.
	free := make(map[string]struct{}, len(names))
	for _, n := range names {
		free[n] = struct{}{}
	}
	var bestProv providers.Provider
	var bestModel string
	var bestCost float64 = -1
	for _, p := range e.registry.All() {
		if _, ok := free[p.Name()]; !ok {
			continue
		}
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

// cheapestPaidProviderModel is the pre-free-providers behaviour, kept as
// a fallback when no free credit is available or none match the protocol.
func (e *Engine) cheapestPaidProviderModel(proto providers.Protocol) (providers.Provider, string) {
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
