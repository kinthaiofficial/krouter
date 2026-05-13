package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// AnnouncementRecord mirrors the announcements table row.
type AnnouncementRecord struct {
	ID          string
	Type        string
	Priority    string
	PublishedAt time.Time
	ExpiresAt   *time.Time
	TitleJSON   string // {"en":"...","zh-CN":"..."}
	SummaryJSON string
	URL         string
	Icon        string
	ReceivedAt  time.Time
	ReadAt      *time.Time
	DismissedAt *time.Time
}

// Title returns the localised title, falling back to the "en" value.
func (a *AnnouncementRecord) Title(lang string) string {
	var m map[string]string
	if err := json.Unmarshal([]byte(a.TitleJSON), &m); err != nil {
		return a.TitleJSON
	}
	if v, ok := m[lang]; ok {
		return v
	}
	return m["en"]
}

// Summary returns the localised summary, falling back to the "en" value.
func (a *AnnouncementRecord) Summary(lang string) string {
	var m map[string]string
	if err := json.Unmarshal([]byte(a.SummaryJSON), &m); err != nil {
		return a.SummaryJSON
	}
	if v, ok := m[lang]; ok {
		return v
	}
	return m["en"]
}

// InsertAnnouncement stores a new announcement. Duplicate IDs are silently ignored.
func (s *Store) InsertAnnouncement(ctx context.Context, a AnnouncementRecord) error {
	const q = `INSERT OR IGNORE INTO announcements
		(id, type, priority, published_at, expires_at,
		 title_json, summary_json, url, icon, received_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`

	var expiresAt any
	if a.ExpiresAt != nil {
		expiresAt = a.ExpiresAt.UTC().Format(time.RFC3339)
	}

	_, err := s.db.ExecContext(ctx, q,
		a.ID, a.Type, a.Priority,
		a.PublishedAt.UTC().Format(time.RFC3339),
		expiresAt,
		a.TitleJSON, a.SummaryJSON,
		a.URL, a.Icon,
		a.ReceivedAt.UTC().Format(time.RFC3339),
	)
	return err
}

// AnnouncementExists returns true if an announcement with the given ID is stored.
func (s *Store) AnnouncementExists(ctx context.Context, id string) (bool, error) {
	const q = `SELECT 1 FROM announcements WHERE id = ? LIMIT 1`
	var dummy int
	err := s.db.QueryRowContext(ctx, q, id).Scan(&dummy)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// ListAnnouncements returns up to limit announcements (unread first, then newest).
// Dismissed announcements are excluded.
func (s *Store) ListAnnouncements(ctx context.Context, limit int) ([]AnnouncementRecord, error) {
	if limit <= 0 {
		limit = 50
	}
	const q = `SELECT
		id, type, priority, published_at,
		expires_at, title_json, summary_json, url, icon,
		received_at, read_at, dismissed_at
		FROM announcements
		WHERE dismissed_at IS NULL
		ORDER BY CASE WHEN read_at IS NULL THEN 0 ELSE 1 END, published_at DESC
		LIMIT ?`

	rows, err := s.db.QueryContext(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []AnnouncementRecord
	for rows.Next() {
		var a AnnouncementRecord
		var pubStr, recStr string
		var expiresStr, readStr, dismissedStr sql.NullString
		if err := rows.Scan(
			&a.ID, &a.Type, &a.Priority, &pubStr,
			&expiresStr, &a.TitleJSON, &a.SummaryJSON, &a.URL, &a.Icon,
			&recStr, &readStr, &dismissedStr,
		); err != nil {
			return nil, err
		}
		if t, err := time.Parse(time.RFC3339, pubStr); err == nil {
			a.PublishedAt = t
		}
		if t, err := time.Parse(time.RFC3339, recStr); err == nil {
			a.ReceivedAt = t
		}
		if expiresStr.Valid {
			if t, err := time.Parse(time.RFC3339, expiresStr.String); err == nil {
				a.ExpiresAt = &t
			}
		}
		if readStr.Valid {
			if t, err := time.Parse(time.RFC3339, readStr.String); err == nil {
				a.ReadAt = &t
			}
		}
		if dismissedStr.Valid {
			if t, err := time.Parse(time.RFC3339, dismissedStr.String); err == nil {
				a.DismissedAt = &t
			}
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// MarkAnnouncementRead sets read_at for the given announcement.
func (s *Store) MarkAnnouncementRead(ctx context.Context, id string) error {
	const q = `UPDATE announcements SET read_at = ? WHERE id = ? AND read_at IS NULL`
	_, err := s.db.ExecContext(ctx, q, time.Now().UTC().Format(time.RFC3339), id)
	return err
}

// MarkAnnouncementDismissed sets dismissed_at for the given announcement.
func (s *Store) MarkAnnouncementDismissed(ctx context.Context, id string) error {
	const q = `UPDATE announcements SET dismissed_at = ? WHERE id = ?`
	_, err := s.db.ExecContext(ctx, q, time.Now().UTC().Format(time.RFC3339), id)
	return err
}

// CountUnreadAnnouncements returns the number of unread, non-dismissed, non-expired announcements.
func (s *Store) CountUnreadAnnouncements(ctx context.Context) (int, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	const q = `SELECT COUNT(*) FROM announcements
		WHERE read_at IS NULL AND dismissed_at IS NULL
		AND (expires_at IS NULL OR expires_at > ?)`
	var n int
	err := s.db.QueryRowContext(ctx, q, now).Scan(&n)
	return n, err
}

// GetFeedMeta returns a value from feed_meta. Returns "" when absent.
func (s *Store) GetFeedMeta(ctx context.Context, key string) (string, error) {
	const q = `SELECT COALESCE(value,'') FROM feed_meta WHERE key = ?`
	var v string
	err := s.db.QueryRowContext(ctx, q, key).Scan(&v)
	if err != nil {
		return "", nil // absent key is not an error
	}
	return v, nil
}

// SetFeedMeta upserts a key in feed_meta.
func (s *Store) SetFeedMeta(ctx context.Context, key, value string) error {
	const q = `INSERT INTO feed_meta (key, value) VALUES (?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`
	_, err := s.db.ExecContext(ctx, q, key, value)
	return err
}
