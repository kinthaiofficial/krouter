-- Migration 002: web UI auth session tables

-- Browser session cookies (8h expiry, in-memory in daemon; table for persistence across restarts).
CREATE TABLE IF NOT EXISTS web_sessions (
    id         TEXT PRIMARY KEY,  -- random 32-byte hex
    created_at INTEGER NOT NULL,  -- Unix timestamp
    expires_at INTEGER NOT NULL   -- Unix timestamp
);
CREATE INDEX IF NOT EXISTS idx_web_sessions_expires ON web_sessions(expires_at);

-- Single-use auth tickets (30s expiry, exchanged for a session cookie).
CREATE TABLE IF NOT EXISTS auth_tickets (
    ticket     TEXT PRIMARY KEY,  -- random 32-byte hex
    created_at INTEGER NOT NULL,  -- Unix timestamp
    expires_at INTEGER NOT NULL,  -- Unix timestamp
    used       INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_auth_tickets_expires ON auth_tickets(expires_at);
