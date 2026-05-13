// Package groq implements the Groq provider adapter.
//
// Groq uses the OpenAI Chat Completions wire format at
// https://api.groq.com/openai, authenticated via GROQ_API_KEY.
//
// See spec/03-providers.md §5.
package groq

import (
	"net/http"

	openaiAdapter "github.com/kinthaiofficial/krouter/internal/providers/openai"
)

const baseURL = "https://api.groq.com/openai"

var supportedModels = []string{
	"llama-3.3-70b-versatile",
	"llama-3.1-8b-instant",
	"mixtral-8x7b-32768",
	"gemma2-9b-it",
}

// New creates a Groq provider adapter.
// If client is nil, a default streaming-safe client is used.
func New(client *http.Client) *openaiAdapter.Adapter {
	return openaiAdapter.New("groq", baseURL, "GROQ_API_KEY", supportedModels, client)
}
