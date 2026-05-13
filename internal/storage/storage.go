// Package storage provides SQLite persistence for krouter.
//
// Database file: ~/.kinthai/data.db (WAL mode, NORMAL sync, 5s busy timeout).
// Schema migrations are embedded and applied automatically on Open.
//
// See spec/05-storage.md for the full schema and retention policy.
package storage

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3" // SQLite driver
	"github.com/oklog/ulid/v2"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Store wraps an SQLite database connection with migration and ID-generation helpers.
type Store struct {
	db      *sql.DB
	mu      sync.Mutex
	entropy *ulid.MonotonicEntropy
}

// Open opens (or creates) the SQLite database at path and applies pending migrations.
// Use ":memory:" for an in-memory database (tests only).
func Open(path string) (*Store, error) {
	dsn := path
	if path != ":memory:" {
		dsn = fmt.Sprintf(
			"file:%s?_journal_mode=WAL&_synchronous=NORMAL&_busy_timeout=5000",
			path,
		)
	}

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.PingContext(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	//nolint:gosec // non-cryptographic random source is fine for ULID entropy
	entropy := ulid.Monotonic(rand.New(rand.NewSource(time.Now().UnixNano())), 0)
	s := &Store{db: db, entropy: entropy}

	if err := s.Migrate(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to apply migrations: %w", err)
	}

	return s, nil
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// DB returns the underlying *sql.DB for direct queries.
func (s *Store) DB() *sql.DB {
	return s.db
}

// NewULID generates a new monotonically increasing ULID string.
// Thread-safe.
func (s *Store) NewULID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return ulid.MustNew(ulid.Timestamp(time.Now()), s.entropy).String()
}

// Migrate applies any unapplied SQL migration files from the embedded migrations/ directory.
// Files are applied in lexical order (001_*.sql, 002_*.sql, …).
// Each migration runs in a transaction; a failure rolls back and stops migration.
func (s *Store) Migrate() error {
	// Bootstrap schema_migrations before running user-defined migrations.
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		applied_at TIMESTAMP NOT NULL
	)`)
	if err != nil {
		return fmt.Errorf("failed to bootstrap schema_migrations: %w", err)
	}

	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("failed to list migration files: %w", err)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}

		version, err := versionFromFilename(name)
		if err != nil {
			return fmt.Errorf("invalid migration file %q: %w", name, err)
		}

		var count int
		if err := s.db.QueryRow(
			`SELECT COUNT(1) FROM schema_migrations WHERE version = ?`, version,
		).Scan(&count); err != nil {
			return fmt.Errorf("failed to check migration %d: %w", version, err)
		}
		if count > 0 {
			continue
		}

		body, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("failed to read migration %q: %w", name, err)
		}

		if err := s.applyMigration(version, string(body)); err != nil {
			return fmt.Errorf("migration %q failed: %w", name, err)
		}
	}

	return nil
}

func (s *Store) applyMigration(version int, sqlBody string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after successful Commit

	if _, err := tx.Exec(sqlBody); err != nil {
		return fmt.Errorf("failed to execute SQL: %w", err)
	}

	if _, err := tx.Exec(
		`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
		version, time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		return fmt.Errorf("failed to record migration version: %w", err)
	}

	return tx.Commit()
}

// versionFromFilename extracts the numeric prefix from a migration filename.
// E.g. "001_initial.sql" → 1.
func versionFromFilename(name string) (int, error) {
	var version int
	if _, err := fmt.Sscanf(name, "%d_", &version); err != nil {
		return 0, fmt.Errorf("filename must start with NNN_: %w", err)
	}
	return version, nil
}
