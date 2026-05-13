package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/kinthaiofficial/krouter/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openMemoryStore(t *testing.T) *storage.Store {
	t.Helper()
	s, err := storage.Open(":memory:")
	require.NoError(t, err)
	require.NoError(t, s.Migrate())
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestInsertRequest_Basic(t *testing.T) {
	s := openMemoryStore(t)

	r := storage.RequestRecord{
		ID:             s.NewULID(),
		Timestamp:      time.Now().UTC(),
		Agent:          "openclaw",
		Protocol:       "anthropic",
		RequestedModel: "claude-sonnet-4-5",
		Provider:       "anthropic",
		Model:          "claude-sonnet-4-5",
		InputTokens:    100,
		OutputTokens:   50,
		LatencyMS:      423,
		StatusCode:     200,
	}

	err := s.InsertRequest(context.Background(), r)
	require.NoError(t, err)

	rows, err := s.ListRequests(context.Background(), 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)

	got := rows[0]
	assert.Equal(t, r.ID, got.ID)
	assert.Equal(t, "openclaw", got.Agent)
	assert.Equal(t, "anthropic", got.Protocol)
	assert.Equal(t, "claude-sonnet-4-5", got.Model)
	assert.Equal(t, 100, got.InputTokens)
	assert.Equal(t, 50, got.OutputTokens)
	assert.Equal(t, int64(423), got.LatencyMS)
	assert.Equal(t, 200, got.StatusCode)
}

func TestListRequests_Ordering(t *testing.T) {
	s := openMemoryStore(t)
	ctx := context.Background()

	base := time.Now().UTC()
	for i := 0; i < 3; i++ {
		err := s.InsertRequest(ctx, storage.RequestRecord{
			ID:        s.NewULID(),
			Timestamp: base.Add(time.Duration(i) * time.Second),
			Protocol:  "anthropic",
			Provider:  "anthropic",
			Model:     "claude-haiku-4-5",
		})
		require.NoError(t, err)
	}

	rows, err := s.ListRequests(ctx, 10)
	require.NoError(t, err)
	require.Len(t, rows, 3)

	// Newest first
	assert.True(t, rows[0].Timestamp.After(rows[1].Timestamp) || rows[0].Timestamp.Equal(rows[1].Timestamp))
	assert.True(t, rows[1].Timestamp.After(rows[2].Timestamp) || rows[1].Timestamp.Equal(rows[2].Timestamp))
}

func TestListRequests_Limit(t *testing.T) {
	s := openMemoryStore(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		err := s.InsertRequest(ctx, storage.RequestRecord{
			ID:        s.NewULID(),
			Timestamp: time.Now().UTC(),
			Protocol:  "anthropic",
			Provider:  "anthropic",
			Model:     "claude-haiku-4-5",
		})
		require.NoError(t, err)
	}

	rows, err := s.ListRequests(ctx, 3)
	require.NoError(t, err)
	assert.Len(t, rows, 3)
}

func TestListRequests_Empty(t *testing.T) {
	s := openMemoryStore(t)
	rows, err := s.ListRequests(context.Background(), 10)
	require.NoError(t, err)
	assert.Empty(t, rows)
}

func TestInsertRequest_ErrorFields(t *testing.T) {
	s := openMemoryStore(t)

	r := storage.RequestRecord{
		ID:           s.NewULID(),
		Timestamp:    time.Now().UTC(),
		Protocol:     "anthropic",
		Provider:     "anthropic",
		Model:        "claude-haiku-4-5",
		StatusCode:   401,
		ErrorMessage: "invalid x-api-key",
	}
	require.NoError(t, s.InsertRequest(context.Background(), r))

	rows, err := s.ListRequests(context.Background(), 1)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, 401, rows[0].StatusCode)
	assert.Equal(t, "invalid x-api-key", rows[0].ErrorMessage)
}
