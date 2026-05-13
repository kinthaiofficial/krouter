// Package providers implements provider adapters for different LLM APIs.
//
// Each provider is a (company, protocol) tuple. Same company with different
// protocols counts as separate providers.
//
// kinthai router NEVER converts between protocols. The provider chosen by the
// routing engine MUST speak the same protocol as the inbound request.
//
// See spec/03-providers.md for adapter contracts and provider list.
package providers

import (
	"context"
	"net/http"
	"sync"
)

// Provider is the interface all provider adapters implement.
type Provider interface {
	Name() string
	Protocol() Protocol
	SupportedModels() []string
	// Forward rewrites the request URL to the provider's endpoint and executes
	// the HTTP call. The caller is responsible for closing resp.Body.
	Forward(ctx context.Context, req *http.Request) (*http.Response, error)
}

// Protocol identifies the wire protocol.
type Protocol string

const (
	ProtocolAnthropic Protocol = "anthropic"
	ProtocolOpenAI    Protocol = "openai"
	ProtocolGemini    Protocol = "gemini"
)

// Registry holds all known providers, keyed by name.
type Registry struct {
	mu        sync.RWMutex
	providers map[string]Provider
}

// New creates an empty provider registry.
func New() *Registry {
	return &Registry{providers: make(map[string]Provider)}
}

// Register adds or replaces a provider in the registry.
func (r *Registry) Register(p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[p.Name()] = p
}

// Get returns the provider with the given name.
func (r *Registry) Get(name string) (Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[name]
	return p, ok
}

// All returns all registered providers.
func (r *Registry) All() []Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Provider, 0, len(r.providers))
	for _, p := range r.providers {
		out = append(out, p)
	}
	return out
}

// ForProtocol returns the first provider that speaks the given protocol.
func (r *Registry) ForProtocol(proto Protocol) (Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, p := range r.providers {
		if p.Protocol() == proto {
			return p, true
		}
	}
	return nil, false
}
