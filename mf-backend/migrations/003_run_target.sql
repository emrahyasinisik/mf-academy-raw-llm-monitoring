-- Records where a run actually executed.
--
-- The same model id can now be run in two places: in the visitor's browser on
-- WebGPU, or on the self-hosted MLC server. Their latencies are not comparable
-- — a browser figure describes the visitor's GPU, a server figure describes one
-- fixed card plus a network hop — so any aggregate that mixes them is
-- meaningless. This column is what lets metrics separate them.
--
-- It is a real column rather than a key inside the free-form metadata JSON
-- precisely because it gets grouped and filtered on; JSON extraction in the
-- aggregate queries would be both slower and unindexable.

ALTER TABLE llm_runs
    ADD COLUMN IF NOT EXISTS target TEXT NOT NULL DEFAULT 'browser';

-- 'browser' is the right default for existing rows: every run recorded before
-- this migration came from WebLLM, since server-side inference did not exist.

ALTER TABLE llm_runs
    DROP CONSTRAINT IF EXISTS llm_runs_target_check;

ALTER TABLE llm_runs
    ADD CONSTRAINT llm_runs_target_check CHECK (target IN ('browser', 'server'));

-- Dashboard aggregates group by target within a user, and the history list can
-- be filtered by it. Left-most column stays user_id because every query is
-- scoped to the owner first.
CREATE INDEX IF NOT EXISTS idx_llm_runs_user_target
    ON llm_runs (user_id, target);
