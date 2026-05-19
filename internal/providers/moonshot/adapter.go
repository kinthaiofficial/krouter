// Package moonshot implements the Moonshot (Kimi) provider adapter.
//
// Moonshot uses the OpenAI Chat Completions wire format at
// https://api.moonshot.cn, authenticated via MOONSHOT_API_KEY.
// Provider name is "moonshot" to match LiteLLM and OpenClaw naming.
//
// See spec/03-providers.md §5.
package moonshot

import (
	"net/http"

	openaiAdapter "github.com/kinthaiofficial/krouter/internal/providers/openai"
)

const baseURL = "https://api.moonshot.cn"

var supportedModels = []string{
	"kimi-k2.5",
	"kimi-k2.6",
	"kimi-latest",
	"kimi-latest-128k",
	"kimi-latest-32k",
	"kimi-latest-8k",
	"moonshot-v1-8k",
	"moonshot-v1-32k",
	"moonshot-v1-128k",
}

// New creates a Moonshot CN provider adapter that reads its key from MOONSHOT_API_KEY.
// If client is nil, a default streaming-safe client is used.
func New(client *http.Client) *openaiAdapter.Adapter {
	return openaiAdapter.New("moonshot", baseURL, "MOONSHOT_API_KEY", supportedModels, client)
}

// NewWithKeyFn creates a Moonshot CN adapter whose API key is retrieved by keyFn at request time.
func NewWithKeyFn(keyFn func() string, client *http.Client) *openaiAdapter.Adapter {
	return openaiAdapter.NewWithKeyFn("moonshot", baseURL, keyFn, supportedModels, client)
}
