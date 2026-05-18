// Package minimax implements the MiniMax provider adapter.
//
// MiniMax exposes an Anthropic-messages compatible API at
// https://api.minimax.chat/anthropic (Chinese mainland platform),
// authenticated via Bearer MINIMAX_API_KEY.
package minimax

import (
	"net/http"

	openaiAdapter "github.com/kinthaiofficial/krouter/internal/providers/openai"
	"github.com/kinthaiofficial/krouter/internal/providers"
)

const baseURL = "https://api.minimax.chat/anthropic"

var supportedModels = []string{
	"MiniMax-M2.7",
	"MiniMax-M2.7-highspeed",
}

// minimaxAdapter wraps the OpenAI adapter but declares Anthropic protocol so
// the routing engine matches it for Anthropic-format inbound requests.
type minimaxAdapter struct {
	*openaiAdapter.Adapter
}

func (m *minimaxAdapter) Protocol() providers.Protocol { return providers.ProtocolAnthropic }

// New creates a MiniMax adapter that reads its key from MINIMAX_API_KEY.
func New(client *http.Client) providers.Provider {
	return &minimaxAdapter{openaiAdapter.New("minimax", baseURL, "MINIMAX_API_KEY", supportedModels, client)}
}

// NewWithKeyFn creates a MiniMax adapter whose API key is retrieved by keyFn at request time.
func NewWithKeyFn(keyFn func() string, client *http.Client) providers.Provider {
	return &minimaxAdapter{openaiAdapter.NewWithKeyFn("minimax", baseURL, keyFn, supportedModels, client)}
}
