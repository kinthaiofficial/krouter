package openai

import "net/http"

const directBaseURL = "https://api.openai.com"

// directModels is the static model list for the direct OpenAI provider.
var directModels = []string{
	"gpt-4o",
	"gpt-4o-mini",
	"gpt-4-turbo",
	"gpt-4",
	"o1",
	"o1-mini",
	"o3-mini",
}

// NewDirect creates an OpenAI provider adapter that reads its key from OPENAI_API_KEY.
func NewDirect(client *http.Client) *Adapter {
	return New("openai", directBaseURL, "OPENAI_API_KEY", directModels, client)
}

// NewDirectWithKeyFn creates an OpenAI adapter whose API key is retrieved by keyFn
// at request time. Prefer this over NewDirect when running as a LaunchAgent.
func NewDirectWithKeyFn(keyFn func() string, client *http.Client) *Adapter {
	return NewWithKeyFn("openai", directBaseURL, keyFn, directModels, client)
}
