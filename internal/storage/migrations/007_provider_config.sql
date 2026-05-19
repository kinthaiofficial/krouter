-- Provider metadata catalog. Stores display names, base URLs and path prefixes
-- for all known providers. Built-in rows are seeded here; users can add custom rows
-- via POST /internal/providers without touching source code.
CREATE TABLE IF NOT EXISTS provider_config (
    name         TEXT PRIMARY KEY,
    display_name TEXT NOT NULL DEFAULT '',
    protocol     TEXT NOT NULL DEFAULT 'openai' CHECK (protocol IN ('openai', 'anthropic')),
    base_url     TEXT NOT NULL DEFAULT '',
    path_prefix  TEXT NOT NULL DEFAULT '',
    is_builtin   INTEGER NOT NULL DEFAULT 0,
    sort_order   INTEGER NOT NULL DEFAULT 100
);

-- Built-in providers (pre-seeded; is_builtin=1 means they cannot be deleted via API).
-- path_prefix="" means no /v1 replacement (standard path passthrough).
-- path_prefix="/v4" etc. replaces the /v1 segment in incoming request paths.
INSERT OR IGNORE INTO provider_config (name, display_name, protocol, base_url, path_prefix, is_builtin, sort_order) VALUES
    ('anthropic',  'Anthropic',        'anthropic', 'https://api.anthropic.com',                   '',                    1,  1),
    ('openai',     'OpenAI',           'openai',    'https://api.openai.com',                       '',                    1,  2),
    ('deepseek',   'DeepSeek',         'openai',    'https://api.deepseek.com',                     '',                    1,  3),
    ('gemini',     'Google Gemini',    'openai',    'https://generativelanguage.googleapis.com',     '/v1beta/openai',      1,  4),
    ('xai',        'xAI (Grok)',       'openai',    'https://api.x.ai',                             '',                    1,  5),
    ('groq',       'Groq',             'openai',    'https://api.groq.com/openai',                  '',                    1,  6),
    ('mistral',    'Mistral AI',       'openai',    'https://api.mistral.ai',                       '',                    1,  7),
    ('moonshot',   'Moonshot AI',      'openai',    'https://api.moonshot.cn',                      '',                    1,  8),
    ('together',   'Together AI',      'openai',    'https://api.together.xyz',                     '',                    1,  9),
    ('fireworks',  'Fireworks AI',     'openai',    'https://api.fireworks.ai/inference',           '',                    1, 10),
    ('perplexity', 'Perplexity',       'openai',    'https://api.perplexity.ai',                    '',                    1, 11),
    ('zai',        'Z.AI (GLM)',       'openai',    'https://open.bigmodel.cn/api/paas',            '/v4',                 1, 12),
    ('qwen',       'Qwen (Alibaba)',   'openai',    'https://dashscope.aliyuncs.com',               '/compatible-mode/v1', 1, 13),
    ('ollama',     'Ollama (local)',   'openai',    'http://localhost:11434',                        '',                    1, 14),
    ('minimax',    'MiniMax',          'anthropic', 'https://api.minimax.chat',                     '',                    1, 15);
