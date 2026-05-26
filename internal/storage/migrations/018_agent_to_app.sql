-- Rename "agent" terminology to "app" throughout the schema.
-- "agent" was a misnomer for the AI application (openclaw, claude-code, cursor);
-- the correct term is "app". True agents are sub-entities inside an app.

ALTER TABLE agent_settings RENAME TO app_settings;
ALTER TABLE app_settings RENAME COLUMN agent_id TO app_id;

ALTER TABLE inherited_endpoints RENAME COLUMN agent_id TO app_id;

ALTER TABLE requests RENAME COLUMN agent TO app;
