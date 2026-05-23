package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/kinthaiofficial/krouter/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBudgetEvent_InsertAndList(t *testing.T) {
	s := openMigratedStore(t)
	ctx := context.Background()

	now := time.Now().UTC()
	require.NoError(t, s.InsertBudgetEvent(ctx, storage.BudgetEvent{
		Timestamp: now.Add(-2 * time.Hour), EventType: storage.BudgetEventWarning80,
		DailyPercent: 0.80, DailyCostUSD: 40, DailyLimitUSD: 50,
	}))
	require.NoError(t, s.InsertBudgetEvent(ctx, storage.BudgetEvent{
		Timestamp: now.Add(-1 * time.Hour), EventType: storage.BudgetEventWarning95,
		DailyPercent: 0.95, DailyCostUSD: 47.5, DailyLimitUSD: 50,
	}))
	require.NoError(t, s.InsertBudgetEvent(ctx, storage.BudgetEvent{
		Timestamp: now, EventType: storage.BudgetEventBlocked,
		DailyPercent: 1.0, DailyCostUSD: 50, DailyLimitUSD: 50,
	}))

	rows, err := s.ListBudgetEvents(ctx, 10)
	require.NoError(t, err)
	require.Len(t, rows, 3)
	// Newest first.
	assert.Equal(t, storage.BudgetEventBlocked, rows[0].EventType)
	assert.Equal(t, storage.BudgetEventWarning95, rows[1].EventType)
	assert.Equal(t, storage.BudgetEventWarning80, rows[2].EventType)
}

func TestBudgetEvent_ListRespectsLimit(t *testing.T) {
	s := openMigratedStore(t)
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		require.NoError(t, s.InsertBudgetEvent(ctx, storage.BudgetEvent{
			Timestamp: time.Now().UTC().Add(time.Duration(-i) * time.Minute),
			EventType: storage.BudgetEventBlocked, DailyPercent: 1.0,
			DailyCostUSD: 50, DailyLimitUSD: 50,
		}))
	}
	rows, err := s.ListBudgetEvents(ctx, 3)
	require.NoError(t, err)
	assert.Len(t, rows, 3)
}

func TestBudgetEvent_ListDefaultsAndCaps(t *testing.T) {
	s := openMigratedStore(t)
	ctx := context.Background()
	// Default (limit <= 0) should be 50.
	rows, err := s.ListBudgetEvents(ctx, 0)
	require.NoError(t, err)
	assert.Empty(t, rows)
	// Over-cap stays under 500.
	rows, err = s.ListBudgetEvents(ctx, 10000)
	require.NoError(t, err)
	assert.Empty(t, rows)
}
