// Package qwen implements the Aliyun Qwen (通义千问) provider adapter.
//
// Qwen uses the OpenAI Chat Completions wire format at
// https://dashscope.aliyuncs.com, authenticated via DASHSCOPE_API_KEY.
// The compatible-mode path is /compatible-mode/v1 rather than /v1.
//
// See spec/03-providers.md §5.
package qwen

import (
	"net/http"

	openaiAdapter "github.com/kinthaiofficial/krouter/internal/providers/openai"
)

const baseURL = "https://dashscope.aliyuncs.com"

var supportedModels = []string{
	"qwen-max",
	"qwen-plus",
	"qwen-turbo",
	"qwen-long",
}

// New creates a Qwen provider adapter that reads its key from DASHSCOPE_API_KEY.
// If client is nil, a default streaming-safe client is used.
func New(client *http.Client) *openaiAdapter.Adapter {
	// Qwen's OpenAI-compatible endpoint uses /compatible-mode/v1 instead of /v1.
	return openaiAdapter.NewWithPathReplace("qwen", baseURL, "/compatible-mode/v1", "DASHSCOPE_API_KEY", supportedModels, client)
}

// NewWithKeyFn creates a Qwen adapter whose API key is retrieved by keyFn at request time.
func NewWithKeyFn(keyFn func() string, client *http.Client) *openaiAdapter.Adapter {
	return openaiAdapter.NewWithPathReplaceAndKeyFn("qwen", baseURL, "/compatible-mode/v1", keyFn, supportedModels, client)
}
