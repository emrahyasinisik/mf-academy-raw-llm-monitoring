-- 002_indexes.sql — access-path indexes for the run history queries.
-- Applied automatically on server start (idempotent).

-- Every run query is scoped to one user and ordered newest-first. With separate
-- user_id and created_at indexes, Postgres scanned the global created_at index
-- and discarded non-matching users as a filter — measured at 980 rows discarded
-- to return 20, a ratio that worsens linearly as users are added. A composite
-- index on (user_id, created_at DESC) lets the same query be answered by
-- positioning once and walking 20 entries.
CREATE INDEX IF NOT EXISTS idx_llm_runs_user_created
    ON llm_runs (user_id, created_at DESC);

-- Same access path with the optional model filter applied.
CREATE INDEX IF NOT EXISTS idx_llm_runs_user_model_created
    ON llm_runs (user_id, model, created_at DESC);

-- Now redundant: both composite indexes above lead with user_id, so either can
-- serve a lookup by user_id alone. Keeping it would only add write amplification.
DROP INDEX IF EXISTS idx_llm_runs_user_id;

-- Likewise redundant: idx_llm_runs_user_created covers ordering within a user,
-- and no query orders by created_at across all users.
DROP INDEX IF EXISTS idx_llm_runs_created_at;

-- Unused: every run query is scoped to a user first, so a model-only index can
-- never be chosen. It only cost write amplification on insert.
DROP INDEX IF EXISTS idx_llm_runs_model;

-- llm_scores.run_id already carries a UNIQUE constraint, which is backed by its
-- own index; the explicit one duplicates it.
DROP INDEX IF EXISTS idx_llm_scores_run_id;

-- Refresh-token lookup happens on every token refresh and must not scan.
-- The hash is unique in practice, so a unique index also enforces that.
CREATE UNIQUE INDEX IF NOT EXISTS idx_sessions_token_hash_unique
    ON sessions (refresh_token_hash);
DROP INDEX IF EXISTS idx_sessions_token_hash;
