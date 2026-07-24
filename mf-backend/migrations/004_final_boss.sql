-- 004_final_boss.sql — admin control plane, PEFT adapter registry, DeepKwiki corpus.
--
-- Three unrelated-looking tables land in one migration because they arrive as
-- one feature: the admin panel edits llm_settings, the adapter registry is what
-- its "Adapter Management" module lists, and DeepKwiki is the thing the tuned
-- adapter is being tuned *for*. Splitting them would produce three files that
-- are never applied independently.

-- Runtime LLM settings -------------------------------------------------
--
-- Deliberately a single row, not a key/value table. The settings are a fixed,
-- known set with different types; a key/value table would store them all as
-- TEXT and move every type error from schema time to runtime. The CHECK on id
-- is what makes "single row" a database guarantee rather than a convention the
-- application is trusted to keep.
--
-- These override the process's environment defaults at request time. That is
-- the point: an operator changing the system prompt must not require a redeploy
-- of a service running on someone else's desktop.
CREATE TABLE IF NOT EXISTS llm_settings (
    id                SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    system_prompt     TEXT NOT NULL DEFAULT '',
    temperature       DOUBLE PRECISION NOT NULL DEFAULT 0.7,
    top_p             DOUBLE PRECISION NOT NULL DEFAULT 0.95,
    max_tokens        INTEGER NOT NULL DEFAULT 512,
    -- The adapter currently being served. NULL means the untuned base model.
    -- ON DELETE SET NULL rather than RESTRICT: deleting an adapter should fall
    -- back to the base model, not fail because it happens to be active.
    active_adapter_id UUID,
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by        UUID REFERENCES users(id) ON DELETE SET NULL
);

-- Bounds enforced here as well as in Go. The Go handler gives a good error
-- message; this stops anything that reaches the database another way (a manual
-- UPDATE, a future migration, a bug) from parking the model on nonsense.
ALTER TABLE llm_settings DROP CONSTRAINT IF EXISTS llm_settings_ranges_check;
ALTER TABLE llm_settings ADD CONSTRAINT llm_settings_ranges_check CHECK (
    temperature >= 0 AND temperature <= 2
    AND top_p > 0 AND top_p <= 1
    AND max_tokens > 0 AND max_tokens <= 8192
);

-- Seed the singleton. ON CONFLICT DO NOTHING keeps this migration idempotent —
-- it is re-run on every boot.
INSERT INTO llm_settings (id) VALUES (1) ON CONFLICT (id) DO NOTHING;

-- PEFT adapter registry ------------------------------------------------
--
-- What this table is NOT: a place adapters get hot-swapped from. MLC compiles
-- the model ahead of time (TVM, quantised weights packed offline), so there is
-- no runtime slot to plug a LoRA into — mlc-ai/mlc-llm#2625 is still open and
-- the runtime-support PR #3281 was closed unmerged in March 2026.
--
-- So a row here is a *build*, and `status` tracks it through the pipeline:
--
--   registered -> training -> merging -> compiling -> ready -> active
--                     |           |          |
--                     +-----------+----------+--> failed
--
-- `mlc_model_id` is what the inference host must be told to serve once the row
-- reaches `ready`; until then it is empty.
CREATE TABLE IF NOT EXISTS llm_adapters (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name           TEXT NOT NULL UNIQUE,
    base_model     TEXT NOT NULL,
    status         TEXT NOT NULL DEFAULT 'registered',
    -- LoRA hyperparameters, kept so a build is reproducible from its row alone.
    lora_rank      INTEGER NOT NULL DEFAULT 16,
    lora_alpha     INTEGER NOT NULL DEFAULT 32,
    target_modules TEXT[] NOT NULL DEFAULT '{q_proj,k_proj,v_proj,o_proj}',
    -- Model id to serve once built; empty until the compile step succeeds.
    mlc_model_id   TEXT NOT NULL DEFAULT '',
    -- Free-form build/eval telemetry (loss curve, schema-adherence before and
    -- after, wall-clock). JSONB because its shape changes as the pipeline grows
    -- and none of it is queried relationally.
    metrics        JSONB NOT NULL DEFAULT '{}',
    notes          TEXT NOT NULL DEFAULT '',
    last_error     TEXT NOT NULL DEFAULT '',
    created_by     UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    activated_at   TIMESTAMPTZ
);

ALTER TABLE llm_adapters DROP CONSTRAINT IF EXISTS llm_adapters_status_check;
ALTER TABLE llm_adapters ADD CONSTRAINT llm_adapters_status_check CHECK (
    status IN ('registered', 'training', 'merging', 'compiling', 'ready', 'active', 'failed')
);

ALTER TABLE llm_adapters DROP CONSTRAINT IF EXISTS llm_adapters_rank_check;
ALTER TABLE llm_adapters ADD CONSTRAINT llm_adapters_rank_check CHECK (
    lora_rank > 0 AND lora_rank <= 256 AND lora_alpha > 0 AND lora_alpha <= 512
);

-- Deferred so the table above can be created first; a self-contained FK in the
-- CREATE would fail on a fresh database where llm_adapters does not exist yet.
ALTER TABLE llm_settings DROP CONSTRAINT IF EXISTS llm_settings_active_adapter_fkey;
ALTER TABLE llm_settings ADD CONSTRAINT llm_settings_active_adapter_fkey
    FOREIGN KEY (active_adapter_id) REFERENCES llm_adapters(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_llm_adapters_status ON llm_adapters(status);
CREATE INDEX IF NOT EXISTS idx_llm_adapters_created_at ON llm_adapters(created_at DESC);

-- Role values -----------------------------------------------------------
--
-- The role column has existed since 001 but nothing constrained or enforced it.
-- Two roles is the whole model: an admin may reach the control plane, a user
-- may not. Anything richer (per-capability grants) would be scaffolding for
-- permissions that do not exist.
--
-- Promotion to admin is deliberately NOT reachable from the API. There is no
-- endpoint that writes this column; the only paths are this migration's sibling
-- bootstrap in cmd/server (which reads ADMIN_EMAIL from the environment) and a
-- manual UPDATE by whoever holds the database credentials. An HTTP route that
-- grants admin is a privilege-escalation bug waiting for its first auth flaw.
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check;
ALTER TABLE users ADD CONSTRAINT users_role_check CHECK (role IN ('user', 'admin'));
