package providers_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/kinthaiofficial/krouter/internal/providers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeProvider is a test double implementing providers.Provider.
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

func TestRegistry_RegisterAndGet(t *testing.T) {
	r := providers.New()
	p := &fakeProvider{name: "anthropic", protocol: providers.ProtocolAnthropic, models: []string{"claude-haiku-4-5"}}
	r.Register(p)

	got, ok := r.Get("anthropic")
	require.True(t, ok)
	assert.Equal(t, "anthropic", got.Name())
}

func TestRegistry_GetMissing(t *testing.T) {
	r := providers.New()
	_, ok := r.Get("nonexistent")
	assert.False(t, ok)
}

func TestRegistry_RegisterOverwrite(t *testing.T) {
	r := providers.New()
	r.Register(&fakeProvider{name: "anthropic", models: []string{"model-a"}})
	r.Register(&fakeProvider{name: "anthropic", models: []string{"model-b"}})

	got, ok := r.Get("anthropic")
	require.True(t, ok)
	assert.Equal(t, []string{"model-b"}, got.SupportedModels())
}

func TestRegistry_All(t *testing.T) {
	r := providers.New()
	r.Register(&fakeProvider{name: "anthropic", protocol: providers.ProtocolAnthropic})
	r.Register(&fakeProvider{name: "deepseek", protocol: providers.ProtocolOpenAI})

	all := r.All()
	assert.Len(t, all, 2)
}

func TestRegistry_ForProtocol(t *testing.T) {
	r := providers.New()
	r.Register(&fakeProvider{name: "anthropic", protocol: providers.ProtocolAnthropic})
	r.Register(&fakeProvider{name: "deepseek", protocol: providers.ProtocolOpenAI})

	p, ok := r.ForProtocol(providers.ProtocolAnthropic)
	require.True(t, ok)
	assert.Equal(t, "anthropic", p.Name())

	_, ok = r.ForProtocol(providers.ProtocolGemini)
	assert.False(t, ok)
}
