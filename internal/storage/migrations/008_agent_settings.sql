-- Migration 008: per-user agent inheritance settings.
-- Stores which AI agents (OpenClaw / Claude Code / Cursor / etc.) the user has
-- chosen to enable, along with the resolved config file path. See spec/04.
--
-- This table is NEVER pre-populated by the daemon. It only contains rows the
-- user has explicitly created via the wizard or dashboard. Default paths for
-- known agents come from internal/agentscan code (Scanner.DefaultConfigPath()).

CREATE TABLE IF NOT EXISTS agent_settings (
    agent_id        TEXT PRIMARY KEY,        -- e.g. "openclaw", "claude-code", "cursor"
    enabled         INTEGER NOT NULL DEFAULT 0,    -- 1 = include in routing
    config_path     TEXT NOT NULL,           -- absolute path, may differ from default
    last_scanned_at INTEGER,                 -- ms UTC of last successful scan
    last_error      TEXT                     -- last scan error, NULL on success
);
