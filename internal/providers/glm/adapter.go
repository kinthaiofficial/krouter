// Package glm implements the Zhipu GLM provider adapter.
//
// Zhipu AI (智谱) uses the OpenAI Chat Completions wire format at
// https://open.bigmodel.cn/api/paas, authenticated via ZHIPU_API_KEY.
// The API version path is /v4 rather than /v1.
//
// See spec/03-providers.md §5.
package glm

import (
	"net/http"

	openaiAdapter "github.com/kinthaiofficial/krouter/internal/providers/openai"
)

const baseURL = "https://open.bigmodel.cn/api/paas"

var supportedModels = []string{
	"glm-4",
	"glm-4-flash",
	"glm-4-air",
	"glm-4-airx",
}

// New creates a Zhipu GLM provider adapter.
// If client is nil, a default streaming-safe client is used.
func New(client *http.Client) *openaiAdapter.Adapter {
	// GLM uses /v4 instead of /v1 for its API path.
	return openaiAdapter.NewWithPathReplace("glm", baseURL, "/v4", "ZHIPU_API_KEY", supportedModels, client)
}
