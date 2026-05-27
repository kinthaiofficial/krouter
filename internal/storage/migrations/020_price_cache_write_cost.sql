-- Migration 020: add cache write price columns to token_price_api.
-- cache_write_input_cost_per_token       : cost per token for creating a cache entry (standard, 5-min TTL).
-- cache_write_input_cost_per_token_1hr   : cost per token for extended cache entries (>1 hr TTL, Anthropic only).
-- Both default to 0 (no cache creation charge) for providers that don't support prompt caching.
ALTER TABLE token_price_api ADD COLUMN cache_write_input_cost_per_token REAL NOT NULL DEFAULT 0;
ALTER TABLE token_price_api ADD COLUMN cache_write_input_cost_per_token_1hr REAL NOT NULL DEFAULT 0;
