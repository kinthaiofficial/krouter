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

func TestComplexityScore_KeywordsIncrease(t *testing.T) {
	withKeyword := routing.Request{SystemPrompt: "please refactor the codebase", InputTokenEst: 3000}
	without := routing.Request{InputTokenEst: 3000}
	assert.Greater(t, routing.ComplexityScore(withKeyword), routing.ComplexityScore(without))
}

func TestComplexityScore_MultipleKeywordsCanCrossThreshold(t *testing.T) {
	// 10 keywords × 0.05 = 0.50 (avoids floating-point 0.39999 edge case at exactly 8).
	req := routing.Request{
		SystemPrompt: "debug and refactor and architect and design and analyze and optimize and implement and review and audit and migration",
	}
	assert.GreaterOrEqual(t, routing.ComplexityScore(req), 0.4)
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

	// Saver downgrades to haiku — reason should mention savings vs opus.
	assert.Equal(t, "claude-haiku-4-5-20251001", dec.Model)
	assert.Contains(t, dec.Reason, "便宜")
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
