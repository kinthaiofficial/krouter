// Package minimax implements the MiniMax provider adapter.
//
// MiniMax exposes an Anthropic-messages compatible API at
// https://api.minimax.io/anthropic, authenticated via MINIMAX_API_KEY.
package minimax

import (
	"net/http"

	anthropicAdapter "github.com/kinthaiofficial/krouter/internal/providers/anthropic"
)

const baseURL = "https://api.minimax.io/anthropic"

var supportedModels = []string{
	"MiniMax-M2.7",
	"MiniMax-M2.7-highspeed",
}

// New creates a MiniMax provider adapter backed by the Anthropic wire protocol.
// API key is read from MINIMAX_API_KEY and injected on every upstream request.
func New(client *http.Client) *anthropicAdapter.Adapter {
	return anthropicAdapter.NewNamed("minimax", baseURL, "MINIMAX_API_KEY", supportedModels, client)
}
