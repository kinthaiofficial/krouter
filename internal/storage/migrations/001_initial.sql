-- Migration 001: initial schema
-- All tables from spec/05-storage.md.
-- schema_migrations is bootstrapped in Go before this runs; not repeated here.

-- Per-request log (cap retention to 90 days via daily cleanup job).
CREATE TABLE IF NOT EXISTS requests (
    id              TEXT PRIMARY KEY,            -- ULID
    ts_utc          TIMESTAMP NOT NULL,
    agent           TEXT,                        -- "openclaw" | "claude-code" | "cursor" | ...
    protocol        TEXT,                        -- "anthropic" | "openai"
    requested_model TEXT,
    actual_provider TEXT,
    actual_model    TEXT,
    input_tokens    INTEGER,
    output_tokens   INTEGER,
    cached_tokens   INTEGER,
    cost_micro_usd  INTEGER,                     -- micro-USD (1e6 = $1)
    latency_ms      INTEGER,
    status_code     INTEGER,
    error_message   TEXT
);
CREATE INDEX IF NOT EXISTS idx_requests_ts       ON requests(ts_utc);
CREATE INDEX IF NOT EXISTS idx_requests_provider ON requests(actual_provider);

-- Quota state: 5h window / weekly / Opus running totals (Anthropic Pro plan).
CREATE TABLE IF NOT EXISTS quota_state (
    window_type  TEXT PRIMARY KEY,               -- "5h" | "weekly" | "opus"
    tokens_used  INTEGER NOT NULL,
    window_start TIMESTAMP NOT NULL,
    window_end   TIMESTAMP NOT NULL,
    updated_at   TIMESTAMP NOT NULL
);

-- Per-provider rolling health stats.
CREATE TABLE IF NOT EXISTS provider_status (
    provider             TEXT PRIMARY KEY,
    last_success_at      TIMESTAMP,
    last_failure_at      TIMESTAMP,
    consecutive_failures INTEGER DEFAULT 0,
    last_error_code      INTEGER,
    rolling_success_rate REAL                    -- last 100 requests
);

-- LLM pricing (synced from LiteLLM JSON every 24h).
CREATE TABLE IF NOT EXISTS pricing_cache (
    model_id                    TEXT PRIMARY KEY,
    provider                    TEXT NOT NULL,
    input_cost_per_token        REAL,
    output_cost_per_token       REAL,
    cached_input_cost_per_token REAL,
    max_tokens                  INTEGER,
    raw_json                    TEXT,            -- original LiteLLM entry for debugging
    updated_at                  TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS pricing_sync_meta (
    key   TEXT PRIMARY KEY,
    value TEXT
    -- Keys: last_sync_at, last_etag, last_sha256, source_url
);

-- Notification center (spec/09-notifications.md).
CREATE TABLE IF NOT EXISTS announcements (
    id           TEXT PRIMARY KEY,
    type         TEXT NOT NULL,                  -- free_credit | provider_news | ...
    priority     TEXT NOT NULL,                  -- low | normal | critical
    published_at TIMESTAMP NOT NULL,
    expires_at   TIMESTAMP,
    title_json   TEXT NOT NULL,                  -- {"en": "...", "zh-CN": "..."}
    summary_json TEXT NOT NULL,
    url          TEXT,
    icon         TEXT,
    read_at      TIMESTAMP,
    dismissed_at TIMESTAMP,
    clicked_at   TIMESTAMP,
    received_at  TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_announcements_unread ON announcements(read_at) WHERE read_at IS NULL;

CREATE TABLE IF NOT EXISTS feed_meta (
    key   TEXT PRIMARY KEY,
    value TEXT
    -- Keys: last_etag, last_polled_at, last_feed_updated_at
);

-- Paired devices for LAN remote access (spec/10-remote-daemon.md, M2+).
CREATE TABLE IF NOT EXISTS paired_devices (
    device_id    TEXT PRIMARY KEY,               -- "dev_xxx_xxx"
    device_name  TEXT NOT NULL,
    token_hash   TEXT NOT NULL,                  -- SHA-256(token), never plaintext
    ip_address   TEXT,                           -- pairing-time IP, for audit only
    paired_at    TIMESTAMP NOT NULL,
    last_seen_at TIMESTAMP,
    user_agent   TEXT
);

-- Internal settings KV (auxiliary to settings.json, for daemon-internal state).
CREATE TABLE IF NOT EXISTS settings_kv (
    key        TEXT PRIMARY KEY,
    value      TEXT,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
