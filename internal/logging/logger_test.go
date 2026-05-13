package logging_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/kinthaiofficial/krouter/internal/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewLogger(t *testing.T) {
	var buf bytes.Buffer
	l := logging.NewWithWriter("info", &buf)
	require.NotNil(t, l)
	l.Info("hello", "key", "value")
	assert.Contains(t, buf.String(), "hello")
	assert.Contains(t, buf.String(), "INFO")
}

func TestLogLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	l := logging.NewWithWriter("info", &buf)
	l.Debug("should not appear")
	assert.Empty(t, buf.String(), "debug message should be filtered at info level")
}

func TestFromContextFallback(t *testing.T) {
	l := logging.FromContext(context.Background())
	require.NotNil(t, l)
}

func TestWithContext(t *testing.T) {
	var buf bytes.Buffer
	original := logging.NewWithWriter("debug", &buf)
	ctx := logging.WithContext(context.Background(), original)
	retrieved := logging.FromContext(ctx)
	require.NotNil(t, retrieved)
	retrieved.Info("round-trip")
	assert.Contains(t, buf.String(), "round-trip")
}

func TestSanitizeHeader(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		expected string
	}{
		{"x-api-key", "sk-secret", "[REDACTED]"},
		{"X-API-KEY", "sk-secret", "[REDACTED]"},
		{"authorization", "Bearer tok", "[REDACTED]"},
		{"Authorization", "Bearer tok", "[REDACTED]"},
		{"content-type", "application/json", "application/json"},
		{"x-request-id", "abc123", "abc123"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := logging.SanitizeHeader(tt.name, tt.value)
			assert.Equal(t, tt.expected, got)
		})
	}
}
