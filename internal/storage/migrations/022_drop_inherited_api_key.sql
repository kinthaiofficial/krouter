-- Migration 022: credentials never persist — drop inherited_endpoints.api_key
-- and scrub oauth_token values from extras_json.
--
-- Inherited credentials (API keys / OAuth tokens scanned from agent configs)
-- now live only in the daemon's memory (agentscan.CredStore), repopulated
-- from the agent config files at startup and on every rescan. This restores
-- D-003 to its unconditional form: no key, in any form, ever touches SQLite.
--
-- The UPDATE runs before the DROP so a database that crashed mid-migration
-- never retains tokens in extras_json while claiming the migration applied.

UPDATE inherited_endpoints
   SET extras_json = json_remove(extras_json, '$.oauth_token')
 WHERE extras_json IS NOT NULL
   AND json_valid(extras_json);

ALTER TABLE inherited_endpoints DROP COLUMN api_key;
