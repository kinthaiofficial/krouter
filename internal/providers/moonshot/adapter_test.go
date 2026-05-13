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
	assert.Equal(t, "moonshot-cn", a.Name())
	assert.Equal(t, providers.ProtocolOpenAI, a.Protocol())
	assert.Contains(t, a.SupportedModels(), "moonshot-v1-8k")
	assert.Contains(t, a.SupportedModels(), "moonshot-v1-32k")
	assert.Contains(t, a.SupportedModels(), "moonshot-v1-128k")
}
