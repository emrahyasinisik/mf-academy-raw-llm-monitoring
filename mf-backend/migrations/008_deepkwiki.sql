-- DeepKwiki: a searchable knowledge base the analysis engine and its users can
-- both cite.
--
-- Retrieval here is PostgreSQL full-text search plus trigram similarity, not
-- vector embeddings. That is a deliberate choice, not a shortcut:
--
--   * Embeddings would need an embedding model. The one card in this system is
--     already holding two inference engines, and a third model competing for
--     6 GB would cost more than the recall it buys.
--   * pgvector is not installed on the local database and cannot be assumed on
--     the hosted one, so a vector index would make the two environments behave
--     differently — the worst property a search feature can have.
--   * The product's whole claim is that every statement is checkable against a
--     verbatim source. Lexical search returns the passage that literally
--     contains the words, which is exactly what a citation needs.
--
-- The cost is real and worth naming: a purely lexical index misses paraphrase.
-- The trigram fallback below covers typos and inflection, not synonymy.

CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE EXTENSION IF NOT EXISTS unaccent;

CREATE TABLE IF NOT EXISTS wiki_documents (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- Stable, human-typable identifier used in URLs and citations. A document
    -- re-ingested under the same slug replaces its chunks rather than
    -- duplicating them, which is what makes ingestion idempotent.
    slug       TEXT NOT NULL UNIQUE,
    title      TEXT NOT NULL,
    -- Where this came from, so a reader can go past our copy to the original.
    -- Empty for pasted text, which is a legitimate source, just not a linkable
    -- one.
    source_url TEXT NOT NULL DEFAULT '',
    -- The full original text. Kept alongside the chunks — the chunks are the
    -- retrieval unit, this is the record of what was actually ingested, and
    -- without it a re-chunk after changing the splitter would have nothing to
    -- work from.
    body       TEXT NOT NULL,
    tags       TEXT[] NOT NULL DEFAULT '{}',
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Chunks are the retrieval unit.
--
-- A whole document is the wrong thing to hand a 2B model with a small context:
-- one long page would fill the prompt and leave no room for the question, and
-- ranking whole documents cannot tell the reader *where* in it the answer is.
CREATE TABLE IF NOT EXISTS wiki_chunks (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    document_id UUID NOT NULL REFERENCES wiki_documents(id) ON DELETE CASCADE,
    -- Position within the document, so retrieved passages can be shown in
    -- reading order and a citation can say "third section" rather than a uuid.
    ordinal     INTEGER NOT NULL,
    -- The nearest heading above this chunk, carried down so a passage retrieved
    -- on its own still says what it is about. Without it a mid-document chunk
    -- reads as a floating paragraph.
    heading     TEXT NOT NULL DEFAULT '',
    body        TEXT NOT NULL,

    -- Generated, not maintained by the application. A trigger or an application
    -- write can be forgotten on one of the paths that insert chunks; a stored
    -- generated column cannot be out of date with the row it belongs to.
    --
    -- The heading is weighted 'B' and the body 'D', so a passage whose *title*
    -- matches the query outranks one that merely mentions the words in passing.
    --
    -- 'turkish' is fixed here, and queries must use the same configuration or
    -- the index cannot serve them. It is not a per-document setting: Turkish is
    -- agglutinative, so "yatırımcılara" and "yatırımcı" are the same word with
    -- different suffixes, and only a stemmer that knows the language matches
    -- them. That single decision buys more recall than anything else in this
    -- file. English text stems mostly to itself under it, which is a graceful
    -- enough degradation for the occasional foreign document.
    tsv tsvector GENERATED ALWAYS AS (
        setweight(to_tsvector('turkish', coalesce(heading, '')), 'B') ||
        setweight(to_tsvector('turkish', coalesce(body, '')), 'D')
    ) STORED,

    UNIQUE (document_id, ordinal)
);

CREATE INDEX IF NOT EXISTS wiki_chunks_tsv_idx ON wiki_chunks USING GIN (tsv);

-- Trigram index over the body, for the query that full-text search cannot
-- answer: a misspelling, or a Turkish suffix the 'simple' stemmer leaves
-- attached. GIN rather than GiST because this table is read far more than
-- written and GIN's larger build cost buys faster lookups.
CREATE INDEX IF NOT EXISTS wiki_chunks_body_trgm_idx
    ON wiki_chunks USING GIN (body gin_trgm_ops);

CREATE INDEX IF NOT EXISTS wiki_chunks_document_idx ON wiki_chunks (document_id, ordinal);

CREATE INDEX IF NOT EXISTS wiki_documents_tags_idx ON wiki_documents USING GIN (tags);
