// Package anthropic implements the Anthropic Messages API provider adapter.
//
// Speaks the Anthropic wire protocol (POST /v1/messages, x-api-key auth).
// See spec/03-providers.md §4.
package anthropic

import (
	"context"
	"fmt"
	"net/http"
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
	baseURL    string
	httpClient *http.Client
}

// New creates an Anthropic adapter.
// baseURL is typically "https://api.anthropic.com"; pass a test server URL for testing.
// If client is nil, a default client with no timeout is used (streaming requires no timeout).
func New(baseURL string, client *http.Client) *Adapter {
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
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: client,
	}
}

func (a *Adapter) Name() string               { return "anthropic" }
func (a *Adapter) Protocol() providers.Protocol { return providers.ProtocolAnthropic }
func (a *Adapter) SupportedModels() []string   { return supportedModels }

// Forward rewrites the request URL to point at the Anthropic base URL, then
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

	resp, err := a.httpClient.Do(upstreamReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic adapter: do request: %w", err)
	}
	return resp, nil
}
