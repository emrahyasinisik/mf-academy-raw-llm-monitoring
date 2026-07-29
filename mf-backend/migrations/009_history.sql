-- 009_history.sql — conversation history for the investment persona.
--
-- Until now /decision/chat was stateless by design: the client held the whole
-- transcript and re-sent it every turn, so the server scaled horizontally and a
-- reload never lost a conversation the client still had. The second half of that
-- sentence is where it broke down as a product — the client only "still has" it
-- until the tab closes. A user who spent six turns researching a company and
-- came back the next morning found an empty screen, and the work that produced
-- the verdict was gone even though every token of it had been paid for on a
-- 6 GB card.
--
-- So the transcript is persisted, but the request shape is not changed: the
-- client still sends the full history and the agent still reads it from the
-- request. Persistence is a side effect of a turn, not the source of truth
-- during one. That keeps the agent exactly as stateless as it was — two
-- instances behind a load balancer still behave identically — and it means a
-- failure to write history degrades to the old behaviour rather than to a lost
-- answer.
--
-- Codegen deliberately gets no table here. Its runs have been recorded in
-- llm_runs since 001, with a `target` column since 003; what that side is
-- missing is a screen, not a schema.

-- Conversations ---------------------------------------------------------
CREATE TABLE IF NOT EXISTS conversations (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    -- Which product owns this thread. Only 'persona' writes here today, but the
    -- column exists from the start because the alternative — adding it once a
    -- second surface needs threads — means backfilling rows whose product can
    -- only be guessed from their content.
    product    TEXT NOT NULL DEFAULT 'persona',

    -- Derived from the opening user message, not asked for. A titling round trip
    -- would add a second generation to the slowest route in the system, and the
    -- first thing a user types here is already a subject line ("Acme AI — seed
    -- aşaması B2B SaaS"). Renameable, so a bad derivation is a nuisance and not
    -- a permanent mislabel.
    title      TEXT NOT NULL DEFAULT '',

    -- The last verdict this thread committed to, mirrored out of the final
    -- assistant message so the list can show a decision badge without reading
    -- every message body. NULL while the persona is still researching or asking
    -- clarifying questions — which is a real state and not a missing value, so
    -- it is nullable rather than defaulted to a fake "undecided" label.
    verdict       TEXT,
    verdict_score INTEGER,

    -- Touched on every turn, so the list can order by activity rather than by
    -- creation. A thread resumed after a week belongs at the top; ordering by
    -- created_at would bury it under threads nobody has opened since.
    last_turn_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE conversations DROP CONSTRAINT IF EXISTS conversations_verdict_score_check;
ALTER TABLE conversations ADD CONSTRAINT conversations_verdict_score_check CHECK (
    verdict_score IS NULL OR (verdict_score >= 0 AND verdict_score <= 100)
);

-- The list query: one user's threads, most recently active first.
CREATE INDEX IF NOT EXISTS idx_conversations_user_active
    ON conversations(user_id, product, last_turn_at DESC);

-- Messages --------------------------------------------------------------
CREATE TABLE IF NOT EXISTS conversation_messages (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,

    -- Position in the thread, 0-based. Not a timestamp: the two turns of one
    -- exchange are written in a single transaction and can land on the same
    -- microsecond, at which point ordering by created_at renders the reply
    -- before the question it answers. A random UUID tie-break would be stable
    -- across queries but still arbitrary, which is the same bug with better
    -- disguise. This column makes replay order a fact the writer states.
    ordinal INTEGER NOT NULL,

    role    TEXT NOT NULL,
    content TEXT NOT NULL,

    -- The research behind an assistant turn: the numbered sources it cited and
    -- the tool calls that found them. Stored as documents rather than as two
    -- child tables because they are written once, read whole with their message,
    -- and never queried across messages — there is no "find every reply citing
    -- this URL" question. Empty arrays on user turns.
    --
    -- This is also the part that cannot be recovered later: the persona
    -- researches live, so re-running a turn tomorrow returns different evidence.
    -- A transcript without its sources would render a verdict whose citations
    -- point at nothing, which is worse than no history — it reads as authority
    -- it no longer has.
    sources  JSONB NOT NULL DEFAULT '[]',
    research JSONB NOT NULL DEFAULT '[]',

    -- Which build produced this turn. Kept per message, not per conversation:
    -- an operator can activate a new adapter mid-thread, and when a verdict is
    -- later disputed the question is which model wrote *that* reply.
    model TEXT NOT NULL DEFAULT '',

    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE conversation_messages DROP CONSTRAINT IF EXISTS conversation_messages_role_check;
ALTER TABLE conversation_messages ADD CONSTRAINT conversation_messages_role_check
    CHECK (role IN ('user', 'assistant'));

-- Replay order within one thread, and the guarantee that two writers cannot
-- claim the same position. The unique constraint is what turns a concurrent
-- double-submit into a failed insert the caller can retry, rather than two
-- messages at ordinal 4 that render in whichever order the planner picks.
CREATE UNIQUE INDEX IF NOT EXISTS idx_conversation_messages_thread
    ON conversation_messages(conversation_id, ordinal);
