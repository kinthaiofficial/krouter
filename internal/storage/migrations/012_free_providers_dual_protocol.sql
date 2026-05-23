-- Follow-up to 011_free_providers.sql.
--
-- Some free-credit vendors expose more than one protocol endpoint on the
-- same account (OpenRouter, GLM/Zhipu, Moonshot all offer an
-- Anthropic-compatible `/messages` alongside their OpenAI-compatible
-- `/chat/completions`). The user must register them as separate provider
-- entries inside their AI agent (same API key, different baseURL); each
-- side then arrives as its own row in `inherited_endpoints` and routing
-- treats them independently to preserve spec/00 §B2 (same-protocol
-- routing).
--
-- This column carries a JSON array of those alternate-protocol entries,
-- e.g. `[{"protocol":"anthropic","krouter_provider_name":"openrouter-anthropic","key_setup_hint":"…"}]`.
-- Empty array `[]` means the provider speaks only the primary protocol
-- column from 011.
--
-- Pure additive ALTER — pre-existing rows keep `[]` and continue to
-- function as single-protocol entries.

ALTER TABLE free_provider_state
    ADD COLUMN additional_protocols_json TEXT NOT NULL DEFAULT '[]';
