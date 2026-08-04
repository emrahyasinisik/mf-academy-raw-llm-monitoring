-- 016_persona_assessment.sql — link a persona thread to its latest rubric report.
--
-- The persona conversation gathers the case; analysis/run produces the auditable
-- report. Without this column the side panel cannot reopen "this thread's
-- report" after a reload. ON DELETE SET NULL: deleting the assessment row must
-- not delete the conversation the operator still wants to read. Redaction does
-- not DELETE the row — the FE clears the link when GET returns redacted/404.

ALTER TABLE conversations
    ADD COLUMN IF NOT EXISTS assessment_id UUID
    REFERENCES assessments(id) ON DELETE SET NULL;
