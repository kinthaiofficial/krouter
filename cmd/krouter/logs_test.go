package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatLogRow(t *testing.T) {
	row := map[string]any{
		"ts":            "2026-07-05T14:23:01Z",
		"provider":      "deepseek",
		"model":         "deepseek-chat",
		"input_tokens":  float64(4200),
		"output_tokens": float64(1100),
		"cost_usd":      0.024,
		"latency_ms":    float64(1800),
		"status_code":   float64(200),
	}
	out := formatLogRow(row)
	assert.Contains(t, out, "deepseek/deepseek-chat")
	assert.Contains(t, out, "4K in / 1K out")
	assert.Contains(t, out, "$0.0240")
	assert.Contains(t, out, "1800ms")
	assert.Contains(t, out, "200")
}

func TestFormatLogRow_SubThousandTokensShowRawCounts(t *testing.T) {
	// Sub-1K counts used to render as "0K", hiding real traffic (and masking
	// the MiniMax zero-usage bug in the 2026-07-05 field report).
	row := map[string]any{
		"ts":            "2026-07-05T14:23:01Z",
		"provider":      "minimax",
		"model":         "MiniMax-M3",
		"input_tokens":  float64(37),
		"output_tokens": float64(16),
		"cost_usd":      0.0001,
		"latency_ms":    float64(400),
		"status_code":   float64(200),
	}
	assert.Contains(t, formatLogRow(row), "37 in / 16 out")
}

func TestScanSSE_DispatchesEventsAndSkipsHeartbeats(t *testing.T) {
	stream := ": ping\n\n" +
		"event: request_completed\n" +
		`data: {"id":1,"provider":"deepseek"}` + "\n\n" +
		"event: config_changed\n" +
		`data: {}` + "\n\n" +
		"event: request_completed\n" +
		`data: {"id":2,"provider":"minimax"}` + "\n\n"

	var got []string
	err := scanSSE(strings.NewReader(stream), func(event string, data []byte) {
		got = append(got, event+"|"+string(data))
	})
	require.NoError(t, err)
	assert.Equal(t, []string{
		`request_completed|{"id":1,"provider":"deepseek"}`,
		`config_changed|{}`,
		`request_completed|{"id":2,"provider":"minimax"}`,
	}, got)
}

func TestScanSSE_EOFMidEventDoesNotDispatchPartial(t *testing.T) {
	// A connection dropped before the terminating blank line must not emit
	// a half-received event.
	stream := "event: request_completed\n" + `data: {"id":1}`
	calls := 0
	err := scanSSE(strings.NewReader(stream), func(string, []byte) { calls++ })
	require.NoError(t, err)
	assert.Zero(t, calls)
}
