-- Migration 009: cache of vendor endpoints extracted from agent configs.
-- Populated by internal/agentscan Scanner implementations on each rescan.
-- See spec/04 and spec/05.

CREATE TABLE IF NOT EXISTS inherited_endpoints (
    agent_id        TEXT NOT NULL,           -- FK to agent_settings.agent_id
    provider        TEXT NOT NULL,           -- "anthropic", "minimax-portal", "openrouter", ...
    endpoint_url    TEXT NOT NULL,           -- upstream base URL discovered from agent config
    protocol_hint   TEXT,                    -- "anthropic-messages", "openai-chat", etc. (optional)
    api_key         TEXT,                    -- static API key, if any
    extras_json     TEXT,                    -- flexible JSON for OAuth tokens, subscription metadata, etc.
    captured_at     INTEGER NOT NULL,        -- ms UTC when this row was last written
    PRIMARY KEY (agent_id, provider),
    FOREIGN KEY (agent_id) REFERENCES agent_settings(agent_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_inherited_endpoints_provider ON inherited_endpoints(provider);
