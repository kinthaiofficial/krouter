package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/kinthaiofficial/krouter/internal/providers"
	openaiadapter "github.com/kinthaiofficial/krouter/internal/providers/openai"
	"github.com/kinthaiofficial/krouter/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeDiscoverer implements providers.Provider + ModelDiscoverer + ModelSetter
// so DiscoverIfStale can be exercised without a real upstream.
type fakeDiscoverer struct {
	models    []string
	discovers int
}

func (f *fakeDiscoverer) Name() string                 { return "fakeprov" }
func (f *fakeDiscoverer) Protocol() providers.Protocol { return providers.ProtocolOpenAI }
func (f *fakeDiscoverer) SupportedModels() []string    { return f.models }
func (f *fakeDiscoverer) SetModels(m []string)         { f.models = m }
func (f *fakeDiscoverer) Forward(context.Context, *http.Request) (*http.Response, error) {
	return nil, nil
}
func (f *fakeDiscoverer) DiscoverModels(_ context.Context, keyFn func() string) ([]providers.ModelInfo, error) {
	f.discovers++
	_ = keyFn()
	return []providers.ModelInfo{{ID: "m1"}, {ID: "m2"}}, nil
}

// TestApplyDiscoveredModelsToRegistry_FeedsAdapter verifies the hard switch:
// the routing engine reads availability from a provider adapter's
// SupportedModels(), and that list must come from /v1/models discovery cached
// in the DB — not from the LiteLLM catalog.
func TestApplyDiscoveredModelsToRegistry_FeedsAdapter(t *testing.T) {
	srv, store := newFPTestServer(t)
	ctx := context.Background()

	reg := providers.New()
	adapter := openaiadapter.New("deepseek", "https://api.deepseek.com", "DEEPSEEK_API_KEY", nil, nil)
	reg.Register(adapter)
	srv.SetRegistry(reg)

	require.Empty(t, adapter.SupportedModels(), "adapter starts with no models")

	require.NoError(t, store.SaveDiscoveredModels(ctx, "deepseek", []storage.DiscoveredModel{
		{Provider: "deepseek", ModelID: "deepseek-chat"},
		{Provider: "deepseek", ModelID: "deepseek-reasoner"},
	}))

	srv.ApplyDiscoveredModelsToRegistry(ctx)

	assert.ElementsMatch(t,
		[]string{"deepseek-chat", "deepseek-reasoner"},
		adapter.SupportedModels(),
		"routing availability must come from discovered_models")
}

func TestDiscoverIfStale_DiscoversAndFeedsRegistry(t *testing.T) {
	srv, _ := newFPTestServer(t)
	reg := providers.New()
	f := &fakeDiscoverer{}
	reg.Register(f)
	srv.SetRegistry(reg)

	srv.DiscoverIfStale(context.Background(), "fakeprov", "sk-key")

	assert.Equal(t, 1, f.discovers, "should discover when no cache exists")
	assert.ElementsMatch(t, []string{"m1", "m2"}, f.SupportedModels(),
		"discovered models must be pushed into the registry")
}

func TestDiscoverIfStale_SkipsWhenFresh(t *testing.T) {
	srv, store := newFPTestServer(t)
	reg := providers.New()
	f := &fakeDiscoverer{models: []string{"existing"}}
	reg.Register(f)
	srv.SetRegistry(reg)

	// A fresh cache entry (fetched_at = now) must short-circuit discovery.
	require.NoError(t, store.SaveDiscoveredModels(context.Background(), "fakeprov",
		[]storage.DiscoveredModel{{Provider: "fakeprov", ModelID: "cached"}}))

	srv.DiscoverIfStale(context.Background(), "fakeprov", "sk-key")

	assert.Equal(t, 0, f.discovers, "fresh cache must skip discovery")
	assert.Equal(t, []string{"existing"}, f.SupportedModels(), "registry must be untouched")
}
