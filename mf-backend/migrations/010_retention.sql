-- Retention and erasure for the hosted demo.
--
-- Deleting a row is not the tool here. assessments is an audit trail on
-- purpose: criteria_snapshot is copied so a later rubric edit cannot rewrite
-- what an old report meant, and raw_response is kept verbatim because it is the
-- only way to tell a bad model from a bad parser. Dropping rows would move
-- every aggregate they feed — schema_valid, and the consistency figure a trial
-- group exists to produce — and half a trial group is worse than none.
--
-- So the personal columns are blanked and the row stays. What is left is an
-- anonymous measurement. redacted_at records that this happened, and when.
ALTER TABLE assessments ADD COLUMN IF NOT EXISTS redacted_at TIMESTAMPTZ;
ALTER TABLE llm_runs    ADD COLUMN IF NOT EXISTS redacted_at TIMESTAMPTZ;

-- Partial indexes: the sweep only ever looks for rows it has not already done,
-- so a row leaves the index the moment it is redacted. A full index on
-- created_at would grow with the table and make the sweep slower every month;
-- this one is bounded by the 30-day window.
CREATE INDEX IF NOT EXISTS idx_assessments_unredacted
    ON assessments (created_at) WHERE redacted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_llm_runs_unredacted
    ON llm_runs (created_at) WHERE redacted_at IS NULL;
