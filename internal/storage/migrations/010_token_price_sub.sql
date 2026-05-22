-- Migration 010: per-call subscription pricing.
-- Sibling table to token_price_api (per-token API pricing from LiteLLM).
-- Rows are seeded by the installer from data/token_price_sub.json at install
-- time so pricing updates don't require a daemon rebuild — bump the JSON, run
-- the installer, done.
--
-- Each row describes one purchasable subscription tier of one vendor. The
-- routing engine and dashboard compute effective per-call cost as:
--   monthly_price_cny × cny_to_usd / (total_count × windows_per_month)
-- where windows_per_month = (30 * 24) / window_hours.
--
-- See spec/05-subscription-quota.md §11 for the derivation and bug history.

CREATE TABLE IF NOT EXISTS token_price_sub (
    provider          TEXT    NOT NULL,    -- "minimax" (matches subscription_quota_cache.provider)
    tier_pattern      TEXT    NOT NULL,    -- glob pattern: "MiniMax-M*", "speech-hd", ...
    total_count       INTEGER NOT NULL,    -- calls purchased per window
    highspeed         INTEGER NOT NULL,    -- 0 = standard, 1 = highspeed variant
    monthly_price_cny REAL    NOT NULL,    -- sticker price in CNY
    window_hours      INTEGER NOT NULL,    -- duration of one quota window (MiniMax = 5)
    cny_to_usd        REAL    NOT NULL,    -- conversion rate used by EffectiveCostUSD
    data_source_url   TEXT,                -- vendor page the price was copied from
    updated_at        INTEGER NOT NULL,    -- ms UTC of last write
    PRIMARY KEY (provider, tier_pattern, total_count, highspeed)
);

CREATE INDEX IF NOT EXISTS idx_token_price_sub_lookup
    ON token_price_sub(provider, total_count, highspeed);
