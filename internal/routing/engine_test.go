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
