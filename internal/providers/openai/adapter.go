// Package openai implements a generic OpenAI Chat Completions provider adapter.
//
// This adapter speaks the OpenAI wire format (POST /v1/chat/completions,
// Authorization: Bearer <key>) and is reusable for any OpenAI-compatible
// provider (DeepSeek, Groq, Moonshot, etc.).
//
// See spec/03-providers.md §2 (OpenAIAdapter).
package openai

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/kinthaiofficial/krouter/internal/providers"
)

// Adapter implements providers.Provider for the OpenAI Chat Completions API.
type Adapter struct {
	name        string // provider name, e.g. "openai", "deepseek"
	baseURL     string // e.g. "https://api.deepseek.com" (no trailing slash, no /v1)
	pathReplace string // if non-empty, replaces the /v1 prefix in incoming paths
	apiKeyEnv   string // env var name for the API key, e.g. "DEEPSEEK_API_KEY"
	apiKeyFn    func() string // optional: overrides apiKeyEnv; called per-request
	models      []string
	httpClient  *http.Client
}

// New creates an OpenAI-compatible adapter that reads its API key from the
// named environment variable at request time.
//
//   - name:      provider name ("deepseek", "openai", etc.)
//   - baseURL:   upstream base URL without path, e.g. "https://api.deepseek.com"
//   - apiKeyEnv: environment variable name holding the API key
//   - models:    list of model IDs this provider supports
//   - client:    HTTP client; nil uses a default streaming-safe client
func New(name, baseURL, apiKeyEnv string, models []string, client *http.Client) *Adapter {
	return NewWithPathReplace(name, baseURL, "", apiKeyEnv, models, client)
}

// NewWithKeyFn creates an OpenAI-compatible adapter whose API key is retrieved
// by calling keyFn at request time (e.g. to read from settings + env fallback).
// Use this instead of New when the key must survive LaunchAgent restarts.
func NewWithKeyFn(name, baseURL string, keyFn func() string, models []string, client *http.Client) *Adapter {
	return newWithPathReplaceAndKeyFn(name, baseURL, "", keyFn, models, client)
}

// NewWithPathReplace creates an OpenAI-compatible adapter with a path prefix override.
// pathReplace replaces the incoming /v1 path prefix; e.g. "/v4" for GLM,
// "/compatible-mode/v1" for Qwen. Empty string means no replacement.
func NewWithPathReplace(name, baseURL, pathReplace, apiKeyEnv string, models []string, client *http.Client) *Adapter {
	a := newWithPathReplaceAndKeyFn(name, baseURL, pathReplace, nil, models, client)
	a.apiKeyEnv = apiKeyEnv
	return a
}

// NewWithPathReplaceAndKeyFn combines path-prefix override with a key function.
func NewWithPathReplaceAndKeyFn(name, baseURL, pathReplace string, keyFn func() string, models []string, client *http.Client) *Adapter {
	return newWithPathReplaceAndKeyFn(name, baseURL, pathReplace, keyFn, models, client)
}

func newWithPathReplaceAndKeyFn(name, baseURL, pathReplace string, keyFn func() string, models []string, client *http.Client) *Adapter {
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
		name:        name,
		baseURL:     strings.TrimRight(baseURL, "/"),
		pathReplace: pathReplace,
		apiKeyFn:    keyFn,
		models:      models,
		httpClient:  client,
	}
}

// HasKey reports whether a non-empty API key is currently available.
// Satisfies the providers.Configurable optional interface.
func (a *Adapter) HasKey() bool { return a.resolveKey() != "" }

// resolveKey returns the API key, preferring apiKeyFn over the env var.
func (a *Adapter) resolveKey() string {
	if a.apiKeyFn != nil {
		return a.apiKeyFn()
	}
	return os.Getenv(a.apiKeyEnv)
}

func (a *Adapter) Name() string                { return a.name }
func (a *Adapter) Protocol() providers.Protocol { return providers.ProtocolOpenAI }
func (a *Adapter) SupportedModels() []string   { return a.models }

// Forward rewrites the request URL to the upstream endpoint, injects the
// provider API key, and executes the HTTP call.
//
// The caller must close resp.Body when done.
func (a *Adapter) Forward(ctx context.Context, req *http.Request) (*http.Response, error) {
	path := req.URL.Path
	if a.pathReplace != "" {
		path = a.pathReplace + strings.TrimPrefix(path, "/v1")
	}
	upstreamURL := a.baseURL + path
	if req.URL.RawQuery != "" {
		upstreamURL += "?" + req.URL.RawQuery
	}

	upstreamReq, err := http.NewRequestWithContext(ctx, req.Method, upstreamURL, req.Body)
	if err != nil {
		return nil, fmt.Errorf("openai adapter %s: build request: %w", a.name, err)
	}
	upstreamReq.Header = req.Header.Clone()

	// Inject OpenAI-style auth. The incoming request may carry Anthropic headers
	// (x-api-key) which must be removed; we substitute our own key.
	upstreamReq.Header.Set("Authorization", "Bearer "+a.resolveKey())
	upstreamReq.Header.Del("x-api-key")
	upstreamReq.Header.Del("anthropic-version")
	upstreamReq.Header.Del("anthropic-beta")

	resp, err := a.httpClient.Do(upstreamReq)
	if err != nil {
		return nil, fmt.Errorf("openai adapter %s: do request: %w", a.name, err)
	}
	return resp, nil
}
