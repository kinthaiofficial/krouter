-- Rename legacy "glm" provider to "zai" in model_discovery.
-- In v2.0.42 the GLM adapter was renamed from "glm" to "zai" to match the
-- Z.AI brand. Rows written before that version still carry provider = "glm".
UPDATE model_discovery SET provider = 'zai' WHERE provider = 'glm';
