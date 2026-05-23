-- Budget threshold transitions, persisted so the dashboard can show a
-- timeline of "when did spending cross 80% / 95% / 100% today, and when
-- did the new UTC day reset the counter?"
--
-- Today these events are only broadcast over SSE (see
-- cmd/krouter/serve.go monitorBudget); clients that connect after the
-- transition lose the history. This table is the persistent companion.
--
-- One row per transition; the monitor goroutine inserts at most once per
-- threshold per day (same dedup it already does for the SSE broadcast).

CREATE TABLE IF NOT EXISTS budget_events (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    ts_utc          INTEGER NOT NULL,        -- ms UTC at the time of the transition
    event_type      TEXT    NOT NULL,        -- 'warning_80' | 'warning_95' | 'blocked' | 'unblocked'
    daily_percent   REAL    NOT NULL,        -- 0.0–1.0+ at the moment of the event
    daily_cost_usd  REAL    NOT NULL,        -- spend so far today (USD) at the moment of the event
    daily_limit_usd REAL    NOT NULL         -- the limit in force when the event fired
);

-- Recent-events scan: dashboard renders the last N by ts_utc DESC.
CREATE INDEX IF NOT EXISTS idx_budget_events_ts
    ON budget_events (ts_utc DESC);
