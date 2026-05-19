package moonshot_test

import (
	"testing"

	"github.com/kinthaiofficial/krouter/internal/providers"
	"github.com/kinthaiofficial/krouter/internal/providers/moonshot"
	"github.com/stretchr/testify/assert"
)

func TestMoonshot_Interface(t *testing.T) {
	a := moonshot.New(nil)
	var _ providers.Provider = a
	assert.Equal(t, "moonshot", a.Name())
	assert.Equal(t, providers.ProtocolOpenAI, a.Protocol())
	assert.Contains(t, a.SupportedModels(), "kimi-latest")
	assert.Contains(t, a.SupportedModels(), "moonshot-v1-8k")
}
