package proxy

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Official Anthropic streams: input/cache usage arrives in message_start,
// message_delta carries only the cumulative output_tokens.
func TestParseAnthropicSSEUsage_OfficialShape(t *testing.T) {
	stream := []byte(`data: {"type":"message_start","message":{"usage":{"input_tokens":20,"cache_read_input_tokens":100,"cache_creation_input_tokens":50,"output_tokens":1}}}

data: {"type":"content_block_delta","delta":{"text":"hi"}}

data: {"type":"message_delta","usage":{"output_tokens":15}}

data: [DONE]

`)
	in, out, cached, cacheWrite := parseAnthropicSSEUsage(stream)
	assert.Equal(t, 20, in)
	assert.Equal(t, 15, out)
	assert.Equal(t, 100, cached)
	assert.Equal(t, 50, cacheWrite)
}

// MiniMax's anthropic-compatible endpoint sends placeholder zeros in
// message_start and the real cumulative usage (including input_tokens and
// cache_read_input_tokens) in the final message_delta. The 2026-07-05 field
// report: all streaming MiniMax requests logged input_tokens=0 / cost 0.
func TestParseAnthropicSSEUsage_MiniMaxUsageInMessageDelta(t *testing.T) {
	stream := []byte(`data: {"type":"message_start","message":{"usage":{"input_tokens":0,"output_tokens":0}}}

data: {"type":"message_delta","usage":{"input_tokens":37,"output_tokens":16,"cache_read_input_tokens":128}}

`)
	in, out, cached, cacheWrite := parseAnthropicSSEUsage(stream)
	assert.Equal(t, 37, in, "input_tokens must come from message_delta when message_start has a placeholder zero")
	assert.Equal(t, 16, out)
	assert.Equal(t, 128, cached)
	assert.Equal(t, 0, cacheWrite)
}

// message_delta usage is cumulative per the Anthropic spec, so an
// implementation emitting periodic deltas must not be double-counted.
func TestParseAnthropicSSEUsage_CumulativeDeltasTakeLast(t *testing.T) {
	stream := []byte(`data: {"type":"message_start","message":{"usage":{"input_tokens":20,"output_tokens":1}}}

data: {"type":"message_delta","usage":{"output_tokens":5}}

data: {"type":"message_delta","usage":{"output_tokens":15}}

`)
	in, out, _, _ := parseAnthropicSSEUsage(stream)
	assert.Equal(t, 20, in)
	assert.Equal(t, 15, out, "cumulative deltas: the last value wins, not the sum")
}

// Rare multi-message streams still accumulate across messages.
func TestParseAnthropicSSEUsage_MultiMessageAccumulates(t *testing.T) {
	stream := []byte(`data: {"type":"message_start","message":{"usage":{"input_tokens":20,"output_tokens":0}}}

data: {"type":"message_delta","usage":{"output_tokens":15}}

data: {"type":"message_start","message":{"usage":{"input_tokens":30,"output_tokens":0}}}

data: {"type":"message_delta","usage":{"output_tokens":10}}

`)
	in, out, _, _ := parseAnthropicSSEUsage(stream)
	assert.Equal(t, 50, in)
	assert.Equal(t, 25, out)
}
