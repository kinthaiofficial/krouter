# spec/05-storage.md — SQLite Storage

**Module**: `internal/storage`
**File**: `~/.kinthai/data.db` (with `~/.kinthai/data.db.wal` etc.)

---

## 1. Why SQLite

- Embedded, no separate process
- WAL mode for concurrent reads with one writer
- Atomic writes via filesystem (durable)
- Cross-platform identical behavior
- No "small DB" overhead compared to BoltDB / Badger

CGO required for `github.com/mattn/go-sqlite3`. Acceptable.

---

## 2. Schema

```sql
-- Per-request log (cap retention to 90 days for size)
CREATE TABLE requests (
    id              TEXT PRIMARY KEY,             -- ULID
    ts_utc          TIMESTAMP NOT NULL,
    agent           TEXT,                          -- "openclaw" | ...
    protocol        TEXT,                          -- "anthropic" | "openai"
    requested_model TEXT,
    actual_provider TEXT,
    actual_model    TEXT,
    input_tokens    INTEGER,
    output_tokens   INTEGER,
    cached_tokens   INTEGER,
    cost_micro_usd  INTEGER,                       -- micro-USD for precision
    latency_ms      INTEGER,
    status_code     INTEGER,
    error_message   TEXT
);
CREATE INDEX idx_requests_ts ON requests(ts_utc);
CREATE INDEX idx_requests_provider ON requests(actual_provider);

-- Quota state (5h / weekly / Opus running totals for Anthropic Pro plan)
CREATE TABLE quota_state (
    window_type     TEXT PRIMARY KEY,              -- "5h" | "weekly" | "opus"
    tokens_used     INTEGER NOT NULL,
    window_start    TIMESTAMP NOT NULL,
    window_end      TIMESTAMP NOT NULL,
    updated_at      TIMESTAMP NOT NULL
);

-- Provider availability cache
CREATE TABLE provider_status (
    provider        TEXT PRIMARY KEY,
    last_success_at TIMESTAMP,
    last_failure_at TIMESTAMP,
    consecutive_failures INTEGER DEFAULT 0,
    last_error_code INTEGER,
    rolling_success_rate REAL                      -- last 100 requests
);

-- LLM pricing (synced from LiteLLM JSON)
CREATE TABLE pricing_cache (
    model_id            TEXT PRIMARY KEY,
    provider            TEXT NOT NULL,
    input_cost_per_token        REAL,
    output_cost_per_token       REAL,
    cached_input_cost_per_token REAL,
    max_tokens                   INTEGER,
    raw_json            TEXT,
    updated_at          TIMESTAMP NOT NULL
);

CREATE TABLE pricing_sync_meta (
    key   TEXT PRIMARY KEY,
    value TEXT
);

-- Notification center (spec 09)
CREATE TABLE announcements (
    id              TEXT PRIMARY KEY,
    type            TEXT NOT NULL,
    priority        TEXT NOT NULL,
    published_at    TIMESTAMP NOT NULL,
    expires_at      TIMESTAMP,
    title_json      TEXT NOT NULL,
    summary_json    TEXT NOT NULL,
    url             TEXT,
    icon            TEXT,
    read_at         TIMESTAMP,
    dismissed_at    TIMESTAMP,
    clicked_at      TIMESTAMP,
    received_at     TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_announcements_unread ON announcements(read_at) WHERE read_at IS NULL;

CREATE TABLE feed_meta (
    key   TEXT PRIMARY KEY,
    value TEXT
);

-- Paired devices for remote daemon (spec 10)
CREATE TABLE paired_devices (
    device_id     TEXT PRIMARY KEY,             -- "dev_xxx_xxx"
    device_name   TEXT NOT NULL,
    token_hash    TEXT NOT NULL,                 -- SHA-256(token), never raw
    ip_address    TEXT,                          -- pairing-time IP, for audit
    paired_at     TIMESTAMP NOT NULL,
    last_seen_at  TIMESTAMP,
    user_agent    TEXT
);
-- NOTE: kinthai router has no kinthai.ai account integration
-- (no cloud sync, no data upload, no leaderboard)

-- Internal settings KV (auxiliary to settings.json)
CREATE TABLE settings_kv (
    key   TEXT PRIMARY KEY,
    value TEXT,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Schema migrations
CREATE TABLE schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at TIMESTAMP NOT NULL
);
```

---

## 3. Migrations

Numbered files in `internal/storage/migrations/`:

```
001_initial.sql
002_add_cached_tokens.sql
003_add_paired_devices.sql
...
```

Each migration is idempotent. On startup, daemon:
1. Reads `schema_migrations` table
2. Applies any newer-numbered file
3. Inserts row into `schema_migrations`

Use `embed.FS` to bundle migrations into binary.

---

## 4. Data retention

| Table | Retention |
|-------|-----------|
| requests | 90 days (configurable via settings) |
| announcements | Permanent (cap at 1000, oldest deleted) |
| quota_state | Permanent (rolling windows) |
| pricing_cache | Permanent (sync replaces) |
| provider_status | Permanent (rolling stats) |
| paired_devices | Permanent until revoked |

Daily cleanup job: `DELETE FROM requests WHERE ts_utc < now() - 90 days;`

---

## 5. Performance considerations

- Open with `_journal_mode=WAL&_synchronous=NORMAL&_busy_timeout=5000`
- Single writer goroutine via channel (avoid SQLite write contention)
- Prepared statements for hot paths (request insert)
- VACUUM weekly (background job, non-blocking)

---

## 6. Backup / Reset

GUI Settings → Advanced → "Export logs" exports requests table as CSV.
GUI Settings → Advanced → "Reset data" deletes data.db (after confirmation).

---

## 7. Test coverage

- Unit: each table's CRUD
- Unit: migration application
- Unit: rolling-window quota computation
- Integration: parallel reads + single write

---

## 8. Open questions

- Should we encrypt SQLite at rest? Probably no — no sensitive data stored
  (no API keys, no message content). Just metadata. The argument for would
  be "defense in depth", but it adds complexity and key management burden.
  Default: NO encryption.
