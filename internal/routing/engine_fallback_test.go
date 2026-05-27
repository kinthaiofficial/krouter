package routing_test

import (
	"testing"

	"github.com/kinthaiofficial/krouter/internal/providers"
	"github.com/kinthaiofficial/krouter/internal/routing"
	"github.com/stretchr/testify/assert"
)

// anthropicMultiModelRegistry creates a registry with anthropic + multi-tier models.
func anthropicMultiModelRegistry() *providers.Registry {
	reg := providers.New()
	reg.Register(&fakeProvider{
		name:     "anthropic",
		protocol: providers.ProtocolAnthropic,
		models: []string{
			"claude-opus-4-5",
			"claude-sonnet-4-6",
			"claude-haiku-4-5-20251001",
		},
	})
	return reg
}

// openAIMultiProviderRegistry has deepseek + moonshot for OpenAI protocol.
func openAIMultiProviderRegistry() *providers.Registry {
	reg := providers.New()
	reg.Register(&fakeProvider{
		name:     "deepseek",
		protocol: providers.ProtocolOpenAI,
		models:   []string{"deepseek-chat"},
	})
	reg.Register(&fakeProvider{
		name:     "moonshot",
		protocol: providers.ProtocolOpenAI,
		models:   []string{"moonshot-v1-8k"},
	})
	return reg
}

// ── FallbackDecide ────────────────────────────────────────────────────────────

func TestFallbackDecide_AnthropicOpusToSonnet(t *testing.T) {
	engine := routing.New(anthropicMultiModelRegistry())
	tried := map[string]bool{"anthropic/claude-opus-4-5": true}

	dec := engine.FallbackDecide(routing.Request{Protocol: "anthropic"}, routing.PresetQuality, tried)

	assert.Equal(t, "anthropic", dec.Provider)
	assert.Equal(t, "claude-sonnet-4-6", dec.Model)
	assert.Contains(t, dec.Reason, "fallback")
}

func TestFallbackDecide_AnthropicSonnetToHaiku(t *testing.T) {
	engine := routing.New(anthropicMultiModelRegistry())
	tried := map[string]bool{"anthropic/claude-sonnet-4-6": true}

	dec := engine.FallbackDecide(routing.Request{Protocol: "anthropic"}, routing.PresetBalanced, tried)

	assert.Equal(t, "anthropic", dec.Provider)
	assert.Equal(t, "claude-haiku-4-5-20251001", dec.Model)
	assert.Contains(t, dec.Reason, "fallback")
}

func TestFallbackDecide_AnthropicHaikuNoFallback(t *testing.T) {
	engine := routing.New(anthropicMultiModelRegistry())
	tried := map[string]bool{"anthropic/claude-haiku-4-5-20251001": true}

	dec := engine.FallbackDecide(routing.Request{Protocol: "anthropic"}, routing.PresetSaver, tried)

	assert.Empty(t, dec.Provider, "haiku is the lowest tier — no further fallback")
}

func TestFallbackDecide_OpenAIDeepSeekToMoonshot(t *testing.T) {
	engine := routing.New(openAIMultiProviderRegistry())
	tried := map[string]bool{"deepseek/deepseek-chat": true}

	dec := engine.FallbackDecide(routing.Request{Protocol: "openai", RequestedModel: "deepseek-chat"}, routing.PresetSaver, tried)

	assert.Equal(t, "moonshot", dec.Provider)
	assert.Contains(t, dec.Reason, "fallback")
}

func TestFallbackDecide_AllProvidersFailed(t *testing.T) {
	engine := routing.New(openAIMultiProviderRegistry())
	tried := map[string]bool{
		"deepseek/deepseek-chat":  true,
		"moonshot/moonshot-v1-8k": true,
	}

	dec := engine.FallbackDecide(routing.Request{Protocol: "openai"}, routing.PresetSaver, tried)

	assert.Empty(t, dec.Provider, "all providers tried — should return empty Decision")
}

func TestFallbackDecide_AnthropicAlreadyTriedSonnet(t *testing.T) {
	engine := routing.New(anthropicMultiModelRegistry())
	tried := map[string]bool{
		"anthropic/claude-opus-4-5":   true,
		"anthropic/claude-sonnet-4-6": true,
	}

	dec := engine.FallbackDecide(routing.Request{Protocol: "anthropic"}, routing.PresetQuality, tried)

	// Sonnet already tried — should skip to haiku.
	assert.Equal(t, "claude-haiku-4-5-20251001", dec.Model)
}

// ── complexityScore / complexityKeywords ─────────────────────────────────────

func TestComplexityScore_ImagesAlwaysComplex(t *testing.T) {
	req := routing.Request{HasImages: true}
	assert.GreaterOrEqual(t, routing.ComplexityScore(req), 0.4)
}

func TestComplexityScore_LargeTokensComplex(t *testing.T) {
	req := routing.Request{InputTokenEst: 15000}
	assert.GreaterOrEqual(t, routing.ComplexityScore(req), 0.4)
}

func TestComplexityScore_SmallTokensSimple(t *testing.T) {
	req := routing.Request{InputTokenEst: 100}
	assert.Less(t, routing.ComplexityScore(req), 0.4)
}

func TestComplexityScore_HarnessBoilerplateNotComplex(t *testing.T) {
	// Regression for #65: a short user task inside a large-tool harness (7k tokens,
	// HasTools=true, system prompt with complexity keywords) must not be classified
	// as complex — those are framework signals, not task signals.
	req := routing.Request{
		InputTokenEst: 7000,
		HasTools:      true,
		SystemPrompt:  "debug and refactor and implement and review",
	}
	assert.Less(t, routing.ComplexityScore(req), 0.4, "harness overhead must not push score to complex")
}

func TestComplexityScore_MediumTokensWithToolsNotComplex(t *testing.T) {
	// 4k–10k + HasTools was +0.35 before the fix, enough to cross 0.4 with one keyword.
	// After the fix it must stay below the threshold.
	req := routing.Request{InputTokenEst: 8000, HasTools: true}
	assert.Less(t, routing.ComplexityScore(req), 0.4)
}

// ── enrichDecision (via Decide with pricing) ─────────────────────────────────

func TestDecision_EstimatedCostUSD_Nonzero(t *testing.T) {
	reg := providers.New()
	reg.Register(&fakeProvider{
		name:     "anthropic",
		protocol: providers.ProtocolAnthropic,
		models:   []string{"claude-haiku-4-5-20251001"},
	})
	engine := routing.New(reg)
	engine.WithPricing(&fakePricing{prices: map[string]float64{
		"claude-haiku-4-5-20251001": 0.8 / 1e6,
	}})

	dec := engine.Decide(routing.Request{
		Protocol:       "anthropic",
		RequestedModel: "claude-haiku-4-5-20251001",
		InputTokenEst:  1000,
	}, routing.PresetSaver)

	assert.Greater(t, dec.EstimatedCostUSD, 0.0)
}

func TestDecision_ReasonContainsSavings(t *testing.T) {
	reg := providers.New()
	reg.Register(&fakeProvider{
		name:     "anthropic",
		protocol: providers.ProtocolAnthropic,
		models:   []string{"claude-opus-4-5", "claude-haiku-4-5-20251001"},
	})
	engine := routing.New(reg)
	engine.WithPricing(&fakePricing{prices: map[string]float64{
		"claude-opus-4-5":           15.0 / 1e6,
		"claude-haiku-4-5-20251001": 0.8 / 1e6,
	}})

	dec := engine.Decide(routing.Request{
		Protocol:       "anthropic",
		RequestedModel: "claude-opus-4-5",
		InputTokenEst:  5000,
	}, routing.PresetSaver)

	// Saver downgrades to haiku; savings % display is intentionally omitted
	// until Phase 3 restores a cache-aware version (see enrichDecision).
	assert.Equal(t, "claude-haiku-4-5-20251001", dec.Model)
	assert.NotEmpty(t, dec.Reason)
	assert.Greater(t, dec.EstimatedCostUSD, 0.0)
}

// ── per-agent routing override ────────────────────────────────────────────────

// fakeOverrides implements routing.OverrideSource for testing.
type fakeOverrides struct {
	overrides map[string][2]string // agent → [alwaysUse, preset]
}

func (f *fakeOverrides) GetRoutingOverride(agent string) (alwaysUse, preset string) {
	if v, ok := f.overrides[agent]; ok {
		return v[0], v[1]
	}
	return "", ""
}

func TestRoutingOverride_AlwaysUse(t *testing.T) {
	reg := providers.New()
	reg.Register(&fakeProvider{
		name:     "deepseek",
		protocol: providers.ProtocolOpenAI,
		models:   []string{"deepseek-chat"},
	})
	engine := routing.New(reg)
	engine.WithOverrides(&fakeOverrides{overrides: map[string][2]string{
		"openclaw": {"deepseek-chat", ""},
	}})

	dec := engine.Decide(routing.Request{
		Protocol:       "openai",
		RequestedModel: "gpt-4o",
		AppID:          "openclaw",
	}, routing.PresetQuality)

	assert.Equal(t, "deepseek", dec.Provider)
	assert.Equal(t, "deepseek-chat", dec.Model)
	assert.Contains(t, dec.Reason, "always_use")
}

func TestRoutingOverride_PresetOverride(t *testing.T) {
	engine := routing.New(anthropicMultiModelRegistry())
	engine.WithOverrides(&fakeOverrides{overrides: map[string][2]string{
		"cursor": {"", "saver"},
	}})

	dec := engine.Decide(routing.Request{
		Protocol:       "anthropic",
		RequestedModel: "claude-opus-4-5",
		AppID:          "cursor",
	}, routing.PresetQuality)

	// Preset overridden to saver → should route to haiku.
	assert.Equal(t, "claude-haiku-4-5-20251001", dec.Model)
}

func TestRoutingOverride_UnknownAgent_NoEffect(t *testing.T) {
	engine := routing.New(anthropicMultiModelRegistry())
	engine.WithOverrides(&fakeOverrides{overrides: map[string][2]string{
		"openclaw": {"deepseek-chat", ""},
	}})

	dec := engine.Decide(routing.Request{
		Protocol:       "anthropic",
		RequestedModel: "claude-sonnet-4-5",
		AppID:          "unknown",
	}, routing.PresetBalanced)

	// Override is only for openclaw — unknown agent should get normal routing.
	assert.Equal(t, "anthropic", dec.Provider)
}
