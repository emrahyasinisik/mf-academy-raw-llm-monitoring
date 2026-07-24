-- Hot-swap: a second serving runtime, and the adapter artefact it consumes.
--
-- Background, because the shape of this migration only makes sense with it.
-- The compiled runtime (MLC) generates its kernels ahead of time, so an adapter
-- can only be served there by merging it into the base and recompiling — a new
-- model, twenty minutes of work, and a container restart to switch. That is
-- fine for production and useless for an operator comparing two adapters.
--
-- The second runtime (llama.cpp) keeps the adapter as separate tensors and adds
-- their contribution during inference, so which adapter is active is a number
-- on a running process. Same adapter, two artefacts, two ways to serve it —
-- hence two columns rather than one, and a switch saying which is in use.

-- The GGUF file name published to the hot-swap runtime, e.g. 'tuned-v1.gguf'.
-- Empty until build_gguf.sh runs. Deliberately alongside mlc_model_id rather
-- than replacing it: one adapter can legitimately have both artefacts, and
-- collapsing them into a single "artifact" column would make it impossible to
-- tell whether a build can be hot-swapped, served compiled, or both.
--
-- Stored as a bare file name, not a path. The directory is the runtime's
-- concern and differs between the container (/models/gguf/adapters) and the
-- host; putting a host path in the database would make the row wrong the moment
-- anything is moved, and a name is all that is needed to match the `path` field
-- the runtime reports back.
ALTER TABLE llm_adapters ADD COLUMN IF NOT EXISTS gguf_adapter TEXT NOT NULL DEFAULT '';

-- Which runtime the analysis path is served by.
--
--   'mlc'      compiled, fast, adapter changes need a rebuild
--   'hotswap'  llama.cpp, slower per token, adapter changes are instant
--
-- A setting rather than a per-request field: it decides which engine every
-- user's work lands on, and letting individual requests choose would make two
-- reports incomparable without either of them saying why. The trial endpoint is
-- the exception and passes an override, because comparing engines is precisely
-- what it is for.
ALTER TABLE llm_settings ADD COLUMN IF NOT EXISTS runtime TEXT NOT NULL DEFAULT 'mlc';

ALTER TABLE llm_settings DROP CONSTRAINT IF EXISTS llm_settings_runtime_check;
ALTER TABLE llm_settings ADD CONSTRAINT llm_settings_runtime_check CHECK (
    runtime IN ('mlc', 'hotswap')
);

-- Only one adapter may be loaded at scale 1 at a time, and the runtime has no
-- memory of which one across restarts — it comes up with every scale at 0. This
-- records the intent so the backend can restore it, which is what makes
-- activation survive a container restart instead of silently reverting to the
-- untuned base while the panel still shows an adapter as active.
ALTER TABLE llm_settings ADD COLUMN IF NOT EXISTS active_gguf_adapter TEXT NOT NULL DEFAULT '';

-- A partial index rather than a plain one: almost every row has the empty
-- default, so an unfiltered index would be mostly one repeated value and would
-- not be used. The lookup that matters is "which adapter owns this file the
-- runtime just reported", which only ever asks about non-empty names.
CREATE INDEX IF NOT EXISTS llm_adapters_gguf_idx
    ON llm_adapters (gguf_adapter) WHERE gguf_adapter <> '';
