// Package deepseek implements the DeepSeek provider adapter.
//
// DeepSeek uses the OpenAI Chat Completions wire format at
// https://api.deepseek.com, authenticated via DEEPSEEK_API_KEY env var.
//
// See spec/03-providers.md §4.
package deepseek

import (
	"net/http"

	openaiAdapter "github.com/kinthaiofficial/krouter/internal/providers/openai"
)

const baseURL = "https://api.deepseek.com"

// supportedModels is the static list of DeepSeek models this adapter handles.
var supportedModels = []string{
	"deepseek-chat",
	"deepseek-coder",
	"deepseek-reasoner",
}

// New creates a DeepSeek provider adapter that reads its key from DEEPSEEK_API_KEY.
// If client is nil, a default streaming-safe client is used.
func New(client *http.Client) *openaiAdapter.Adapter {
	return openaiAdapter.New("deepseek", baseURL, "DEEPSEEK_API_KEY", supportedModels, client)
}

// NewWithKeyFn creates a DeepSeek adapter whose API key is retrieved by keyFn
// at request time. Prefer this over New when running as a LaunchAgent.
func NewWithKeyFn(keyFn func() string, client *http.Client) *openaiAdapter.Adapter {
	return openaiAdapter.NewWithKeyFn("deepseek", baseURL, keyFn, supportedModels, client)
}
