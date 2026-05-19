-- model_catalog stores the full model list from LiteLLM's pricing JSON.
-- litellm_provider is the raw value from the "litellm_provider" field (e.g. "dashscope", "zai").
-- model_id has the provider prefix stripped (e.g. "qwen-max" not "dashscope/qwen-max").
CREATE TABLE IF NOT EXISTS model_catalog (
    litellm_provider      TEXT    NOT NULL,
    model_id              TEXT    NOT NULL,
    raw_key               TEXT    NOT NULL DEFAULT '',
    input_cost_per_token  REAL    NOT NULL DEFAULT 0,
    output_cost_per_token REAL    NOT NULL DEFAULT 0,
    max_tokens            INTEGER NOT NULL DEFAULT 0,
    updated_at            TEXT    NOT NULL,
    PRIMARY KEY (litellm_provider, model_id)
);
CREATE INDEX IF NOT EXISTS idx_model_catalog_provider ON model_catalog(litellm_provider);
