-- Acceptance of the terms and of having read the privacy notice.
--
-- Two columns rather than one boolean. The record exists to answer "what did
-- they accept", and a timestamp alone cannot: edit the text and every past
-- acceptance silently becomes an acceptance of the new wording. The version
-- pins it.
--
-- Nullable rather than NOT NULL DEFAULT now(): existing rows have not accepted
-- anything, and defaulting them would manufacture a record of something that
-- never happened. They are gated at login instead.
ALTER TABLE users ADD COLUMN IF NOT EXISTS terms_accepted_at TIMESTAMPTZ;
ALTER TABLE users ADD COLUMN IF NOT EXISTS terms_version     TEXT NOT NULL DEFAULT '';
