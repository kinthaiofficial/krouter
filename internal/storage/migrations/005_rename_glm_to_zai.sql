-- Rename legacy "glm" provider to "zai" in model_discovery.
-- In v2.0.42 the GLM adapter was renamed from "glm" to "zai" to match the
-- Z.AI brand. Rows written before that version still carry provider = "glm".
--
-- Idempotent: delete any glm rows that would conflict with existing zai rows
-- (can happen when both were inserted on a DB that saw a partial migration),
-- then rename remaining glm rows to zai.
DELETE FROM model_discovery
WHERE provider = 'glm'
  AND model_id IN (SELECT model_id FROM model_discovery WHERE provider = 'zai');
UPDATE model_discovery SET provider = 'zai' WHERE provider = 'glm';
