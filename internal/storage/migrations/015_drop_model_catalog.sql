-- Migration 015: drop the redundant model_catalog table.
--
-- model_catalog was write-only dead weight: the LiteLLM pricing sync populated
-- it, but nothing read it for routing. The routable model list now comes from
-- live /v1/models discovery (model_discovery table → provider adapters), and
-- per-model pricing / model counts come from token_price_api. See spec/04.
DROP TABLE IF EXISTS model_catalog;
