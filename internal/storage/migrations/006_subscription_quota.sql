-- Subscription quota cache for call-count-based providers (e.g. MiniMax Coding Plan).
-- Populated by a background poller; used by the routing engine to prioritise these
-- providers when quota is available (effective cost per call ≈ $0.000031).
CREATE TABLE IF NOT EXISTS subscription_quota_cache (
    provider         TEXT NOT NULL,
    model_pattern    TEXT NOT NULL,    -- e.g. "MiniMax-M*", "MiniMax-M2.7-highspeed"
    window_start     INTEGER NOT NULL, -- ms UTC
    window_end       INTEGER NOT NULL, -- ms UTC
    total_count      INTEGER NOT NULL, -- calls allowed in this window (600/1500/4500/30000)
    used_count       INTEGER NOT NULL, -- calls used so far
    highspeed        INTEGER NOT NULL DEFAULT 0, -- 1 = highspeed plan, 0 = standard
    fetched_at       INTEGER NOT NULL, -- ms UTC when this row was last refreshed
    PRIMARY KEY (provider, model_pattern)
);
