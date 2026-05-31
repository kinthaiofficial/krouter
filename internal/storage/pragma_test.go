package storage

import (
	"path/filepath"
	"testing"
)

// TestOpen_PragmasApplied guards against a silent DSN regression: the connection
// pragmas must actually take effect under modernc.org/sqlite. Before the fix the
// DSN used mattn-style _journal_mode=/_busy_timeout= keys that modernc ignored,
// leaving journal_mode=delete + busy_timeout=0 — which made concurrent access
// during the cold-start pricing seed write fail with SQLITE_BUSY ("database error").
func TestOpen_PragmasApplied(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "pragma.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()

	var journalMode string
	if err := s.db.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("query journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Errorf("journal_mode = %q, want \"wal\" (writers would block readers otherwise)", journalMode)
	}

	var busyTimeout int
	if err := s.db.QueryRow("PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
		t.Fatalf("query busy_timeout: %v", err)
	}
	if busyTimeout != 5000 {
		t.Errorf("busy_timeout = %d, want 5000 (concurrent access fails immediately otherwise)", busyTimeout)
	}
}
