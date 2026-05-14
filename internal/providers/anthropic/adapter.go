// Package anthropic implements the Anthropic Messages API provider adapter.
//
// Speaks the Anthropic wire protocol (POST /v1/messages, x-api-key auth).
// See spec/03-providers.md §4.
package anthropic

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/kinthaiofficial/krouter/internal/providers"
)

// supportedModels is the static list of Anthropic models this adapter handles.
// Updated as Anthropic releases new models.
var supportedModels = []string{
	"claude-opus-4-7",
	"claude-sonnet-4-6",
	"claude-haiku-4-5-20251001",
	"claude-opus-4-5",
	"claude-sonnet-4-5",
	"claude-haiku-4-5",
	"claude-3-5-sonnet-20241022",
	"claude-3-5-haiku-20241022",
	"claude-3-opus-20240229",
}

// Adapter implements providers.Provider for the Anthropic Messages API.
type Adapter struct {
	name       string // provider name; defaults to "anthropic"
	baseURL    string
	apiKeyEnv  string // if non-empty, override x-api-key with os.Getenv(apiKeyEnv)
	models     []string
	httpClient *http.Client
}

// New creates an Anthropic adapter targeting https://api.anthropic.com.
// baseURL is typically "https://api.anthropic.com"; pass a test server URL for testing.
// If client is nil, a default client with no timeout is used (streaming requires no timeout).
func New(baseURL string, client *http.Client) *Adapter {
	return NewNamed("anthropic", baseURL, "", supportedModels, client)
}

// NewNamed creates a named Anthropic-protocol adapter, optionally injecting an API key.
// Useful for Anthropic-compatible providers like MiniMax.
//   - name:      provider name in the registry
//   - baseURL:   upstream base URL without trailing slash
//   - apiKeyEnv: env var name for API key injection (empty = pass through client key)
//   - models:    list of model IDs this provider handles
func NewNamed(name, baseURL, apiKeyEnv string, models []string, client *http.Client) *Adapter {
	if client == nil {
		client = &http.Client{
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		}
	}
	return &Adapter{
		name:       name,
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKeyEnv:  apiKeyEnv,
		models:     models,
		httpClient: client,
	}
}

func (a *Adapter) Name() string                { return a.name }
func (a *Adapter) Protocol() providers.Protocol { return providers.ProtocolAnthropic }
func (a *Adapter) SupportedModels() []string {
	if a.models != nil {
		return a.models
	}
	return supportedModels
}

// Forward rewrites the request URL to point at the upstream base URL, then
// executes the HTTP call and returns the full response.
// The caller is responsible for closing resp.Body.
func (a *Adapter) Forward(ctx context.Context, req *http.Request) (*http.Response, error) {
	upstreamURL := a.baseURL + req.URL.Path
	if req.URL.RawQuery != "" {
		upstreamURL += "?" + req.URL.RawQuery
	}

	upstreamReq, err := http.NewRequestWithContext(ctx, req.Method, upstreamURL, req.Body)
	if err != nil {
		return nil, fmt.Errorf("anthropic adapter: build request: %w", err)
	}
	upstreamReq.Header = req.Header.Clone()

	// Inject API key from env if configured (overrides client-supplied key).
	if a.apiKeyEnv != "" {
		if key := os.Getenv(a.apiKeyEnv); key != "" {
			upstreamReq.Header.Set("x-api-key", key)
		}
	}

	resp, err := a.httpClient.Do(upstreamReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic adapter: build request: %w", err)
	}
	return resp, nil
}
