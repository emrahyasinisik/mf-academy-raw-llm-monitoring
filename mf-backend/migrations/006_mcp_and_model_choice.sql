-- 006_mcp_and_model_choice.sql — operator control over which model runs and
-- which MCP servers may be reached.

-- Explicit model choice ------------------------------------------------
--
-- Until now the served model was implied: whichever adapter was active, else a
-- constant compiled into the binary. That conflates two decisions an operator
-- makes separately — "use the tuned build" and "use this model" — and left no
-- way to run the untuned base deliberately while an adapter existed.
--
-- Empty means "no explicit choice", which falls through to the active adapter
-- and then to the compiled default. Not NULL: the column is read on every
-- generation and a nullable text here would make every caller handle a pointer
-- to express a state the empty string already expresses.
ALTER TABLE llm_settings
    ADD COLUMN IF NOT EXISTS default_model TEXT NOT NULL DEFAULT '';

-- MCP servers ----------------------------------------------------------
--
-- Two kinds of row live here and the difference matters.
--
-- `internal` is this service's own MCP endpoint. It is listed so the panel can
-- show it and so a client can discover it, but it cannot be deleted and its URL
-- is not editable — it is a fact about the deployment, not a setting.
--
-- `external` is somebody else's server that the frontend may connect to. These
-- are the ones an operator adds, and the reason this table exists at all: a
-- browser client asking "which MCP servers am I allowed to use" must get its
-- answer from the server, not from a hard-coded list shipped in the bundle,
-- because a list in the bundle cannot be changed without a redeploy and cannot
-- be trusted to have been obeyed.
CREATE TABLE IF NOT EXISTS mcp_servers (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug        TEXT NOT NULL UNIQUE,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',

    kind        TEXT NOT NULL DEFAULT 'external',
    -- Where the client should connect. Empty for the internal server, whose
    -- address is the deployment's own /mcp and therefore already known to
    -- whoever is asking.
    url         TEXT NOT NULL DEFAULT '',
    transport   TEXT NOT NULL DEFAULT 'http',

    -- Which side may use it. A server the Go backend calls and one the browser
    -- connects to have completely different reachability and trust: the browser
    -- needs CORS and a public address, the backend does not. Storing the
    -- intended side stops a localhost-only server being advertised to browsers
    -- that can never reach it.
    side        TEXT NOT NULL DEFAULT 'frontend',

    enabled     BOOLEAN NOT NULL DEFAULT false,
    -- Free-form: headers a client should send, tool allow-lists, notes. JSONB
    -- because its shape is per-server and none of it is queried relationally.
    config      JSONB NOT NULL DEFAULT '{}',

    created_by  UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE mcp_servers DROP CONSTRAINT IF EXISTS mcp_servers_kind_check;
ALTER TABLE mcp_servers ADD CONSTRAINT mcp_servers_kind_check
    CHECK (kind IN ('internal', 'external'));

ALTER TABLE mcp_servers DROP CONSTRAINT IF EXISTS mcp_servers_side_check;
ALTER TABLE mcp_servers ADD CONSTRAINT mcp_servers_side_check
    CHECK (side IN ('frontend', 'backend', 'both'));

ALTER TABLE mcp_servers DROP CONSTRAINT IF EXISTS mcp_servers_transport_check;
ALTER TABLE mcp_servers ADD CONSTRAINT mcp_servers_transport_check
    CHECK (transport IN ('http', 'sse', 'stdio'));

-- An external server with no address cannot be connected to, and one that is
-- enabled and unreachable is worse than one that is absent: the client shows it
-- in a picker and every call fails. Enforced in SQL because the panel is not
-- the only thing that can write this table.
ALTER TABLE mcp_servers DROP CONSTRAINT IF EXISTS mcp_servers_external_url_check;
ALTER TABLE mcp_servers ADD CONSTRAINT mcp_servers_external_url_check
    CHECK (kind = 'internal' OR url <> '');

CREATE INDEX IF NOT EXISTS idx_mcp_servers_enabled ON mcp_servers(enabled, side);

-- The deployment's own server. Enabled by default because it needs no
-- configuration and describes the service the client is already talking to;
-- everything else starts disabled and must be switched on deliberately.
INSERT INTO mcp_servers (slug, name, description, kind, url, side, enabled)
VALUES (
    'mf-analysis',
    'MasterFabric Analiz Motoru',
    'Bu servisin kendi MCP sunucusu: rubrik listesi, vaka analizi ve rapor okuma araçları.',
    'internal',
    '',
    'both',
    true
) ON CONFLICT (slug) DO NOTHING;
