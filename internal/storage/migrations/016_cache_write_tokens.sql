-- Add cache_write_tokens column to track cache_creation_input_tokens separately
-- from cached_tokens (which is cache_read_input_tokens). Cache writes cost 1.25x
-- the regular input price (5m TTL), so they need to be billed separately.
-- Note: input_tokens now stores only fresh tokens (not cached, not written to cache).
ALTER TABLE requests ADD COLUMN cache_write_tokens INTEGER NOT NULL DEFAULT 0;
