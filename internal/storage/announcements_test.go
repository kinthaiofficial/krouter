package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/kinthaiofficial/krouter/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeAnnouncement(id, priority string) storage.AnnouncementRecord {
	return storage.AnnouncementRecord{
		ID:          id,
		Type:        "provider_news",
		Priority:    priority,
		PublishedAt: time.Now().UTC().Add(-time.Hour),
		TitleJSON:   `{"en":"Test","zh-CN":"测试"}`,
		SummaryJSON: `{"en":"Body","zh-CN":"内容"}`,
		URL:         "https://example.com",
		Icon:        "🔔",
		ReceivedAt:  time.Now().UTC(),
	}
}

func TestInsertAndExists(t *testing.T) {
	s := openMigratedStore(t)
	ctx := context.Background()

	a := makeAnnouncement("ann-001", "normal")
	require.NoError(t, s.InsertAnnouncement(ctx, a))

	exists, err := s.AnnouncementExists(ctx, "ann-001")
	require.NoError(t, err)
	assert.True(t, exists)

	exists2, err := s.AnnouncementExists(ctx, "ann-999")
	require.NoError(t, err)
	assert.False(t, exists2)
}

func TestInsertDuplicateIgnored(t *testing.T) {
	s := openMigratedStore(t)
	ctx := context.Background()

	a := makeAnnouncement("ann-dup", "normal")
	require.NoError(t, s.InsertAnnouncement(ctx, a))
	// Second insert should not error.
	require.NoError(t, s.InsertAnnouncement(ctx, a))

	recs, err := s.ListAnnouncements(ctx, 10)
	require.NoError(t, err)
	assert.Len(t, recs, 1)
}

func TestListAnnouncements_UnreadFirst(t *testing.T) {
	s := openMigratedStore(t)
	ctx := context.Background()

	require.NoError(t, s.InsertAnnouncement(ctx, makeAnnouncement("ann-read", "normal")))
	require.NoError(t, s.InsertAnnouncement(ctx, makeAnnouncement("ann-unread", "normal")))

	// Mark ann-read as read.
	require.NoError(t, s.MarkAnnouncementRead(ctx, "ann-read"))

	recs, err := s.ListAnnouncements(ctx, 10)
	require.NoError(t, err)
	require.Len(t, recs, 2)
	// Unread should come first.
	assert.Equal(t, "ann-unread", recs[0].ID)
}

func TestListAnnouncements_ExcludesDismissed(t *testing.T) {
	s := openMigratedStore(t)
	ctx := context.Background()

	require.NoError(t, s.InsertAnnouncement(ctx, makeAnnouncement("ann-dismissed", "normal")))
	require.NoError(t, s.InsertAnnouncement(ctx, makeAnnouncement("ann-visible", "normal")))
	require.NoError(t, s.MarkAnnouncementDismissed(ctx, "ann-dismissed"))

	recs, err := s.ListAnnouncements(ctx, 10)
	require.NoError(t, err)
	require.Len(t, recs, 1)
	assert.Equal(t, "ann-visible", recs[0].ID)
}

func TestCountUnread(t *testing.T) {
	s := openMigratedStore(t)
	ctx := context.Background()

	require.NoError(t, s.InsertAnnouncement(ctx, makeAnnouncement("a1", "normal")))
	require.NoError(t, s.InsertAnnouncement(ctx, makeAnnouncement("a2", "normal")))
	require.NoError(t, s.InsertAnnouncement(ctx, makeAnnouncement("a3", "normal")))

	n, err := s.CountUnreadAnnouncements(ctx)
	require.NoError(t, err)
	assert.Equal(t, 3, n)

	require.NoError(t, s.MarkAnnouncementRead(ctx, "a1"))
	n2, err := s.CountUnreadAnnouncements(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, n2)
}

func TestCountUnread_ExcludesExpired(t *testing.T) {
	s := openMigratedStore(t)
	ctx := context.Background()

	past := time.Now().UTC().Add(-time.Hour)
	a := makeAnnouncement("ann-expired", "normal")
	a.ExpiresAt = &past
	require.NoError(t, s.InsertAnnouncement(ctx, a))

	n, err := s.CountUnreadAnnouncements(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

func TestFeedMeta_GetSet(t *testing.T) {
	s := openMigratedStore(t)
	ctx := context.Background()

	v, err := s.GetFeedMeta(ctx, "last_etag")
	require.NoError(t, err)
	assert.Empty(t, v)

	require.NoError(t, s.SetFeedMeta(ctx, "last_etag", `"abc123"`))
	v2, err := s.GetFeedMeta(ctx, "last_etag")
	require.NoError(t, err)
	assert.Equal(t, `"abc123"`, v2)
}

func TestAnnouncementRecord_Title(t *testing.T) {
	a := storage.AnnouncementRecord{TitleJSON: `{"en":"Hello","zh-CN":"你好"}`}
	assert.Equal(t, "Hello", a.Title("en"))
	assert.Equal(t, "你好", a.Title("zh-CN"))
	assert.Equal(t, "Hello", a.Title("fr")) // falls back to en
}
