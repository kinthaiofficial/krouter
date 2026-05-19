-- Migration 003: model discovery cache
-- Stores the result of /v1/models calls for each provider.
-- Refreshed on agent connect and periodically in the background.

CREATE TABLE IF NOT EXISTS model_discovery (
    provider     TEXT NOT NULL,
    model_id     TEXT NOT NULL,
    display_name TEXT NOT NULL DEFAULT '',
    fetched_at   INTEGER NOT NULL,   -- Unix timestamp (seconds)
    PRIMARY KEY (provider, model_id)
);
