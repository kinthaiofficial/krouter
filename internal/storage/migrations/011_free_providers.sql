-- Free token provider discovery (spec/06).
--
-- Two tables:
--
--   * `free_provider_state` caches the parsed data/free_tokens.json so the
--     API can serve `/internal/free-providers` without re-parsing the file
--     on every request, and so routing can `JOIN inherited_endpoints` to
--     decide which inherited providers are "free credit" providers.
--
--   * `provider_exhausted_until` records when an upstream returned an
--     auth/quota/rate-limit error for a free provider, so routing can
--     fall back to paid candidates until the marked time. TTL is enforced
--     at read time (caller compares NOW() to the column).

CREATE TABLE IF NOT EXISTS free_provider_state (
    id                       TEXT PRIMARY KEY,
    display_name             TEXT NOT NULL,
    krouter_provider_name    TEXT NOT NULL,
    protocol                 TEXT NOT NULL,
    region                   TEXT NOT NULL,
    free_type                TEXT NOT NULL,
    free_summary             TEXT NOT NULL,
    free_quota_usd           REAL NOT NULL DEFAULT 0,
    validity                 TEXT NOT NULL DEFAULT '',
    conditions               TEXT NOT NULL DEFAULT '',
    signup_url               TEXT NOT NULL,
    key_setup_hint           TEXT NOT NULL DEFAULT '',
    active                   INTEGER NOT NULL DEFAULT 1,
    last_verified            TEXT NOT NULL DEFAULT '',
    notes                    TEXT NOT NULL DEFAULT '',
    updated_at               INTEGER NOT NULL
);

-- Lookup by krouter_provider_name is the routing-engine hot path:
-- "is this provider name a known free-credit provider?"
CREATE INDEX IF NOT EXISTS idx_free_provider_state_krouter_name
    ON free_provider_state (krouter_provider_name);

CREATE TABLE IF NOT EXISTS provider_exhausted_until (
    provider          TEXT PRIMARY KEY,
    exhausted_until   INTEGER NOT NULL,   -- ms UTC; row valid until this point
    last_reason       TEXT NOT NULL DEFAULT '',
    last_status_code  INTEGER NOT NULL DEFAULT 0,
    updated_at        INTEGER NOT NULL
);
