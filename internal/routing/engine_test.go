package routing_test

import (
	"context"
	"net/http"
	"os"
	"testing"

	"github.com/kinthaiofficial/krouter/internal/providers"
	"github.com/kinthaiofficial/krouter/internal/routing"
	"github.com/stretchr/testify/assert"
)

// fakeProvider satisfies providers.Provider for testing.
type fakeProvider struct {
	name     string
	protocol providers.Protocol
	models   []string
}

func (f *fakeProvider) Name() string                    { return f.name }
func (f *fakeProvider) Protocol() providers.Protocol    { return f.protocol }
func (f *fakeProvider) SupportedModels() []string       { return f.models }
func (f *fakeProvider) Forward(_ context.Context, _ *http.Request) (*http.Response, error) {
	return nil, nil
}

func anthropicRegistry() *providers.Registry {
	reg := providers.New()
	reg.Register(&fakeProvider{
		name:     "anthropic",
		protocol: providers.ProtocolAnthropic,
		models: []string{
			"claude-opus-4-5",
			"claude-sonnet-4-5",
			"claude-haiku-4-5",
			"claude-haiku-4-5-20251001",
		},
	})
	return reg
}

func multiProviderRegistry() *providers.Registry {
	reg := providers.New()
	reg.Register(&fakeProvider{
		name:     "anthropic",
		protocol: providers.ProtocolAnthropic,
		models:   []string{"claude-opus-4-5", "claude-sonnet-4-5", "claude-haiku-4-5-20251001"},
	})
	reg.Register(&fakeProvider{
		name:     "deepseek",
		protocol: providers.ProtocolOpenAI,
		models:   []string{"deepseek-chat", "deepseek-coder"},
	})
	return reg
}

func TestEngine_Balanced_KnownModel(t *testing.T) {
	engine := routing.New(anthropicRegistry())

	dec := engine.Decide(routing.Request{
		Protocol:       "anthropic",
		RequestedModel: "claude-sonnet-4-5",
	}, routing.PresetBalanced)

	assert.Equal(t, "anthropic", dec.Provider)
	assert.Equal(t, "claude-sonnet-4-5", dec.Model)
	assert.Contains(t, dec.Reason, "Balanced")
}

func TestEngine_Balanced_UnknownModelFallback(t *testing.T) {
	engine := routing.New(anthropicRegistry())

	dec := engine.Decide(routing.Request{
		Protocol:       "anthropic",
		RequestedModel: "claude-future-9000",
	}, routing.PresetBalanced)

	assert.Equal(t, "anthropic", dec.Provider)
	assert.Equal(t, "claude-haiku-4-5-20251001", dec.Model)
	assert.Contains(t, dec.Reason, "claude-future-9000")
}

func TestEngine_Balanced_NoProviderForProtocol(t *testing.T) {
	engine := routing.New(anthropicRegistry())

	dec := engine.Decide(routing.Request{
		Protocol:       "openai",
		RequestedModel: "gpt-4o",
	}, routing.PresetBalanced)

	// Should not panic; return a graceful decision.
	assert.Equal(t, "gpt-4o", dec.Model)
	assert.Contains(t, dec.Reason, "no provider")
}

func TestEngine_Decide_TableDriven(t *testing.T) {
	engine := routing.New(anthropicRegistry())

	tests := []struct {
		name           string
		requestedModel string
		wantModel      string
		wantProvider   string
	}{
		{"haiku passthrough", "claude-haiku-4-5", "claude-haiku-4-5", "anthropic"},
		{"sonnet passthrough", "claude-sonnet-4-5", "claude-sonnet-4-5", "anthropic"},
		{"opus passthrough", "claude-opus-4-5", "claude-opus-4-5", "anthropic"},
		{"unknown falls back", "unknown-model", "claude-haiku-4-5-20251001", "anthropic"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dec := engine.Decide(routing.Request{
				Protocol:       "anthropic",
				RequestedModel: tc.requestedModel,
			}, routing.PresetBalanced)
			assert.Equal(t, tc.wantModel, dec.Model)
			assert.Equal(t, tc.wantProvider, dec.Provider)
		})
	}
}

func TestEngine_Saver_AnthropicDowngradesModel(t *testing.T) {
	engine := routing.New(anthropicRegistry())

	dec := engine.Decide(routing.Request{
		Protocol:       "anthropic",
		RequestedModel: "claude-sonnet-4-5",
	}, routing.PresetSaver)

	assert.Equal(t, "anthropic", dec.Provider)
	assert.Equal(t, "claude-haiku-4-5-20251001", dec.Model)
	assert.Contains(t, dec.Reason, "Saver")
}

func TestEngine_Saver_MultimodalStaysSonnet(t *testing.T) {
	engine := routing.New(anthropicRegistry())

	dec := engine.Decide(routing.Request{
		Protocol:       "anthropic",
		RequestedModel: "claude-opus-4-5",
		HasImages:      true,
	}, routing.PresetSaver)

	assert.Equal(t, "anthropic", dec.Provider)
	assert.Equal(t, "claude-sonnet-4-5", dec.Model)
	assert.Contains(t, dec.Reason, "multimodal")
}

func TestEngine_Saver_OpenAIUsesDeepSeek(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "sk-test")

	engine := routing.New(multiProviderRegistry())

	dec := engine.Decide(routing.Request{
		Protocol:       "openai",
		RequestedModel: "gpt-4o",
	}, routing.PresetSaver)

	assert.Equal(t, "deepseek", dec.Provider)
	assert.Equal(t, "deepseek-chat", dec.Model)
}

func TestEngine_Saver_OpenAINoDeepSeekFallback(t *testing.T) {
	// No DEEPSEEK_API_KEY set.
	old := os.Getenv("DEEPSEEK_API_KEY")
	os.Unsetenv("DEEPSEEK_API_KEY")
	defer os.Setenv("DEEPSEEK_API_KEY", old)

	engine := routing.New(multiProviderRegistry())

	dec := engine.Decide(routing.Request{
		Protocol:       "openai",
		RequestedModel: "gpt-4o",
	}, routing.PresetSaver)

	// Falls back to the registered OpenAI-protocol provider (deepseek, even without key check passing).
	// The important thing is we don't panic.
	assert.NotEmpty(t, dec.Provider)
}

func TestEngine_Quality_ComplexUpgrades(t *testing.T) {
	engine := routing.New(anthropicRegistry())

	dec := engine.Decide(routing.Request{
		Protocol:       "anthropic",
		RequestedModel: "claude-sonnet-4-5",
		HasImages:      true,
	}, routing.PresetQuality)

	assert.Equal(t, "anthropic", dec.Provider)
	assert.Equal(t, "claude-opus-4-5", dec.Model)
	assert.Contains(t, dec.Reason, "Quality")
}

func TestEngine_Quality_SimpleHonorsRequest(t *testing.T) {
	engine := routing.New(anthropicRegistry())

	dec := engine.Decide(routing.Request{
		Protocol:       "anthropic",
		RequestedModel: "claude-haiku-4-5",
	}, routing.PresetQuality)

	assert.Equal(t, "anthropic", dec.Provider)
	assert.Equal(t, "claude-haiku-4-5", dec.Model)
}

func TestEngine_DefaultPresetIsBalanced(t *testing.T) {
	engine := routing.New(anthropicRegistry())

	dec := engine.Decide(routing.Request{
		Protocol:       "anthropic",
		RequestedModel: "claude-sonnet-4-5",
	}, "")

	// Empty preset falls through to Balanced.
	assert.Equal(t, "claude-sonnet-4-5", dec.Model)
	assert.Contains(t, dec.Reason, "Balanced")
}

// mixedAnthropicRegistry registers a real Anthropic provider plus a MiniMax-like provider
// that speaks the Anthropic protocol but does NOT list claude-haiku-4-5-20251001.
func mixedAnthropicRegistry() *providers.Registry {
	reg := providers.New()
	reg.Register(&fakeProvider{
		name:     "anthropic",
		protocol: providers.ProtocolAnthropic,
		models:   []string{"claude-opus-4-5", "claude-sonnet-4-5", "claude-haiku-4-5-20251001"},
	})
	reg.Register(&fakeProvider{
		name:     "minimax",
		protocol: providers.ProtocolAnthropic,
		models:   []string{"MiniMax-Text-01", "abab6.5s-chat"},
	})
	return reg
}

func TestEngine_Saver_AnthropicDoesNotRouteToMiniMax(t *testing.T) {
	engine := routing.New(mixedAnthropicRegistry())

	dec := engine.Decide(routing.Request{
		Protocol:       "anthropic",
		RequestedModel: "claude-sonnet-4-5",
	}, routing.PresetSaver)

	// Must route to anthropic, not minimax.
	assert.Equal(t, "anthropic", dec.Provider)
	assert.Equal(t, "claude-haiku-4-5-20251001", dec.Model)
}

// ── Pricing-tier routing ──────────────────────────────────────────────────────

// fakePricing implements routing.PricingSource for testing.
type fakePricing struct {
	prices map[string]float64 // model → input cost per token
}

func (f *fakePricing) InputCostPerToken(model string) float64 {
	return f.prices[model]
}

func TestEngine_Saver_LivePricingPicksCheapest(t *testing.T) {
	reg := providers.New()
	reg.Register(&fakeProvider{
		name:     "anthropic",
		protocol: providers.ProtocolAnthropic,
		models:   []string{"claude-opus-4-5", "claude-sonnet-4-5", "claude-haiku-4-5-20251001"},
	})
	engine := routing.New(reg)
	engine.WithPricing(&fakePricing{prices: map[string]float64{
		"claude-opus-4-5":           15.0 / 1e6,
		"claude-sonnet-4-5":         3.0 / 1e6,
		"claude-haiku-4-5-20251001": 0.8 / 1e6, // cheapest
	}})

	dec := engine.Decide(routing.Request{
		Protocol:       "anthropic",
		RequestedModel: "claude-sonnet-4-5",
	}, routing.PresetSaver)

	assert.Equal(t, "anthropic", dec.Provider)
	assert.Equal(t, "claude-haiku-4-5-20251001", dec.Model)
	assert.Contains(t, dec.Reason, "live pricing")
}

func TestEngine_Quality_LivePricingUpgradesComplex(t *testing.T) {
	reg := providers.New()
	reg.Register(&fakeProvider{
		name:     "anthropic",
		protocol: providers.ProtocolAnthropic,
		models:   []string{"claude-haiku-4-5-20251001", "claude-sonnet-4-5", "claude-opus-4-5"},
	})
	engine := routing.New(reg)
	engine.WithPricing(&fakePricing{prices: map[string]float64{
		"claude-haiku-4-5-20251001": 0.8 / 1e6,
		"claude-sonnet-4-5":         3.0 / 1e6,
		"claude-opus-4-5":           15.0 / 1e6, // most expensive
	}})

	dec := engine.Decide(routing.Request{
		Protocol:       "anthropic",
		RequestedModel: "claude-sonnet-4-5",
		HasImages:      true, // complex request
	}, routing.PresetQuality)

	assert.Equal(t, "anthropic", dec.Provider)
	assert.Equal(t, "claude-opus-4-5", dec.Model)
	assert.Contains(t, dec.Reason, "live pricing")
}

func TestEngine_Saver_NoPricingFallsBackToHardcoded(t *testing.T) {
	// No pricing attached → should fall back to hardcoded saverAnthropicModel.
	engine := routing.New(anthropicRegistry())

	dec := engine.Decide(routing.Request{
		Protocol:       "anthropic",
		RequestedModel: "claude-sonnet-4-5",
	}, routing.PresetSaver)

	assert.Equal(t, "anthropic", dec.Provider)
	assert.Equal(t, "claude-haiku-4-5-20251001", dec.Model)
	assert.NotContains(t, dec.Reason, "live pricing")
}

func TestEngine_Saver_LivePricingCrossProtocol(t *testing.T) {
	// DeepSeek is cheaper than the OpenAI model; pricing should prefer it.
	reg := providers.New()
	reg.Register(&fakeProvider{
		name:     "openai",
		protocol: providers.ProtocolOpenAI,
		models:   []string{"gpt-4o", "gpt-4o-mini"},
	})
	reg.Register(&fakeProvider{
		name:     "deepseek",
		protocol: providers.ProtocolOpenAI,
		models:   []string{"deepseek-chat"},
	})
	engine := routing.New(reg)
	engine.WithPricing(&fakePricing{prices: map[string]float64{
		"gpt-4o":       2.5 / 1e6,
		"gpt-4o-mini":  0.15 / 1e6,
		"deepseek-chat": 0.14 / 1e6, // cheapest across all OpenAI providers
	}})

	dec := engine.Decide(routing.Request{
		Protocol:       "openai",
		RequestedModel: "gpt-4o",
	}, routing.PresetSaver)

	assert.Equal(t, "deepseek", dec.Provider)
	assert.Equal(t, "deepseek-chat", dec.Model)
	assert.Contains(t, dec.Reason, "live pricing")
}

// ── Budget hard-stop ──────────────────────────────────────────────────────────

// stubQuota implements routing.QuotaSource for testing.
type stubQuota struct{ state routing.QuotaState }

func (s *stubQuota) CurrentQuota(_ context.Context) routing.QuotaState { return s.state }

func TestEngine_BudgetExceeded_DailyBlocksRequest(t *testing.T) {
	engine := routing.New(anthropicRegistry())
	engine.WithQuota(&stubQuota{state: routing.QuotaState{DailyPercent: 1.0}})

	dec := engine.Decide(routing.Request{
		Protocol:       "anthropic",
		RequestedModel: "claude-sonnet-4-5",
	}, routing.PresetBalanced)

	assert.True(t, dec.BudgetExceeded, "daily >= 100% must set BudgetExceeded")
	assert.Equal(t, "", dec.Provider, "blocked decision must have no provider")
}

func TestEngine_BudgetExceeded_WeeklyBlocksRequest(t *testing.T) {
	engine := routing.New(anthropicRegistry())
	engine.WithQuota(&stubQuota{state: routing.QuotaState{WeeklyPercent: 1.05}})

	dec := engine.Decide(routing.Request{
		Protocol:       "anthropic",
		RequestedModel: "claude-sonnet-4-5",
	}, routing.PresetQuality)

	assert.True(t, dec.BudgetExceeded)
}

func TestEngine_BudgetNotExceeded_RoutesNormally(t *testing.T) {
	engine := routing.New(anthropicRegistry())
	engine.WithQuota(&stubQuota{state: routing.QuotaState{DailyPercent: 0.99}})

	dec := engine.Decide(routing.Request{
		Protocol:       "anthropic",
		RequestedModel: "claude-sonnet-4-5",
	}, routing.PresetBalanced)

	assert.False(t, dec.BudgetExceeded, "99% should still route normally")
	assert.NotEmpty(t, dec.Provider)
}

func TestEngine_NoBudgetConfigured_RoutesNormally(t *testing.T) {
	// When no quota source is wired (DailyPercent == 0 == "not configured"),
	// requests must never be blocked regardless of the zero value.
	engine := routing.New(anthropicRegistry())
	engine.WithQuota(&stubQuota{state: routing.QuotaState{}})

	dec := engine.Decide(routing.Request{
		Protocol:       "anthropic",
		RequestedModel: "claude-sonnet-4-5",
	}, routing.PresetBalanced)

	assert.False(t, dec.BudgetExceeded)
	assert.NotEmpty(t, dec.Provider)
}
