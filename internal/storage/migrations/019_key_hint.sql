-- Add key_hint (last 4 chars of the api_key used in the request) to support
-- per-channel statistics under each app. NULL means pre-migration row (unknown).
ALTER TABLE requests ADD COLUMN key_hint TEXT;
