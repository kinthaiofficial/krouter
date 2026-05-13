// Package moonshot implements the Moonshot CN provider adapter.
//
// Moonshot CN uses the OpenAI Chat Completions wire format at
// https://api.moonshot.cn, authenticated via MOONSHOT_API_KEY.
// Primarily used by China-based users.
//
// See spec/03-providers.md §5.
package moonshot

import (
	"net/http"

	openaiAdapter "github.com/kinthaiofficial/krouter/internal/providers/openai"
)

const baseURL = "https://api.moonshot.cn"

var supportedModels = []string{
	"moonshot-v1-8k",
	"moonshot-v1-32k",
	"moonshot-v1-128k",
}

// New creates a Moonshot CN provider adapter.
// If client is nil, a default streaming-safe client is used.
func New(client *http.Client) *openaiAdapter.Adapter {
	return openaiAdapter.New("moonshot-cn", baseURL, "MOONSHOT_API_KEY", supportedModels, client)
}
