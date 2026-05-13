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

// New creates a DeepSeek provider adapter.
// If client is nil, a default streaming-safe client is used.
func New(client *http.Client) *openaiAdapter.Adapter {
	return openaiAdapter.New("deepseek", baseURL, "DEEPSEEK_API_KEY", supportedModels, client)
}
