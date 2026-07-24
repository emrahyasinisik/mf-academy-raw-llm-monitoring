package wiki

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNoRows means the document does not exist.
var ErrNoRows = errors.New("document not found")

// searchConfig is the text-search configuration. It must match the one the
// generated tsvector column in migration 008 was built with — a query using a
// different configuration produces differently-stemmed lexemes, matches almost
// nothing, and does so without any error to explain why.
const searchConfig = "turkish"

// Document is one ingested source.
type Document struct {
	ID        string    `json:"id"`
	Slug      string    `json:"slug"`
	Title     string    `json:"title"`
	SourceURL string    `json:"source_url"`
	Tags      []string  `json:"tags"`
	Chunks    int       `json:"chunks"`
	Body      string    `json:"body,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Hit is one retrieved passage.
type Hit struct {
	DocumentSlug string `json:"document_slug"`
	Title        string `json:"title"`
	SourceURL    string `json:"source_url"`
	Ordinal      int    `json:"ordinal"`
	Heading      string `json:"heading"`
	// Body is verbatim. It is what an answer is allowed to quote and what the
	// reader checks the answer against, so it is never trimmed or rewritten.
	Body string `json:"body"`
	// Snippet is the same passage with the matched terms marked, for display
	// only. Separate from Body precisely so nothing quotes the marked-up form.
	Snippet string  `json:"snippet"`
	Rank    float64 `json:"rank"`
	// Matched records which retrieval pass produced this passage. The three have
	// very different precision, and a reader deserves to know whether a result
	// contains everything they asked for or merely resembles it.
	Matched string `json:"matched"` // "all" | "any" | "fuzzy"
}

// Store owns the SQL for DeepKwiki.
type Store struct{ db *pgxpool.Pool }

func NewStore(db *pgxpool.Pool) *Store { return &Store{db: db} }

// Ingest stores a document and its chunks, replacing any previous version.
//
// Idempotent by slug. Re-ingesting the same document replaces its chunks rather
// than appending, so a corrected source does not leave the old text searchable
// alongside the new — the failure mode where a knowledge base cites a passage
// that no longer exists in the document it names.
//
// One transaction, because a document whose chunks were deleted but not
// reinserted is worse than either state on its own: it is present in listings,
// returns nothing in search, and looks like a retrieval bug.
func (s *Store) Ingest(ctx context.Context, userID string, d Document) (Document, error) {
	chunks := Split(d.Body)

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Document{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var id string
	err = tx.QueryRow(ctx, `
		INSERT INTO wiki_documents (slug, title, source_url, body, tags, created_by)
		     VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (slug) DO UPDATE SET
		    title      = EXCLUDED.title,
		    source_url = EXCLUDED.source_url,
		    body       = EXCLUDED.body,
		    tags       = EXCLUDED.tags,
		    updated_at = now()
		  RETURNING id`,
		d.Slug, d.Title, d.SourceURL, d.Body, d.Tags, nullIfEmpty(userID)).Scan(&id)
	if err != nil {
		return Document{}, err
	}

	if _, err := tx.Exec(ctx, `DELETE FROM wiki_chunks WHERE document_id = $1`, id); err != nil {
		return Document{}, err
	}

	// A batch rather than one round trip per chunk: a long document produces
	// dozens, and on a hosted database each round trip is a network hop.
	batch := &pgx.Batch{}
	for _, c := range chunks {
		batch.Queue(`INSERT INTO wiki_chunks (document_id, ordinal, heading, body)
		             VALUES ($1,$2,$3,$4)`, id, c.Ordinal, c.Heading, c.Body)
	}
	if batch.Len() > 0 {
		if err := tx.SendBatch(ctx, batch).Close(); err != nil {
			return Document{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return Document{}, err
	}
	return s.Get(ctx, d.Slug)
}

const documentColumns = `
	d.id, d.slug, d.title, d.source_url, d.tags, d.created_at, d.updated_at,
	(SELECT count(*) FROM wiki_chunks c WHERE c.document_id = d.id)`

// Get returns one document with its full body.
func (s *Store) Get(ctx context.Context, slug string) (Document, error) {
	var d Document
	err := s.db.QueryRow(ctx,
		`SELECT`+documentColumns+`, d.body FROM wiki_documents d WHERE d.slug = $1`, slug).
		Scan(&d.ID, &d.Slug, &d.Title, &d.SourceURL, &d.Tags, &d.CreatedAt, &d.UpdatedAt,
			&d.Chunks, &d.Body)
	if errors.Is(err, pgx.ErrNoRows) {
		return Document{}, ErrNoRows
	}
	return d, err
}

// List returns the catalogue. Bodies are omitted — a listing that carried every
// document's full text would grow without bound and none of it is displayed.
func (s *Store) List(ctx context.Context) ([]Document, error) {
	rows, err := s.db.Query(ctx,
		`SELECT`+documentColumns+` FROM wiki_documents d ORDER BY d.updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Document{}
	for rows.Next() {
		var d Document
		if err := rows.Scan(&d.ID, &d.Slug, &d.Title, &d.SourceURL, &d.Tags,
			&d.CreatedAt, &d.UpdatedAt, &d.Chunks); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) Delete(ctx context.Context, slug string) error {
	tag, err := s.db.Exec(ctx, `DELETE FROM wiki_documents WHERE slug = $1`, slug)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNoRows
	}
	return nil
}

const hitColumns = `
	d.slug, d.title, d.source_url, c.ordinal, c.heading, c.body`

// Search retrieves passages for a query.
//
// Three passes, each running only if the one before it found nothing:
//
//	all    every query term present in the passage. Precise; when it hits,
//	       the passage is almost certainly about the question.
//	any    at least one term present, ranked by how many. Recall.
//	fuzzy  trigram similarity. Typos, and the Turkish suffixes the stemmer
//	       does not fold, at the cost of matching things that merely look alike.
//
// The middle pass is not an embellishment — without it the feature barely
// works. plainto_tsquery ANDs its terms, so a natural question like "LoRA rank
// ve alpha nasıl seçilir" requires one passage containing all four words, and
// real prose splits them across paragraphs. Measured against this corpus, the
// AND-only version answered one question in four; the questions it dropped were
// well covered by documents it had indexed.
//
// The passes stay separate rather than being unioned because their scores are
// on incompatible scales — ts_rank_cd against word_similarity — and mixing them
// would order results by an incoherent number while looking authoritative.
func (s *Store) Search(ctx context.Context, query string, limit int) ([]Hit, error) {
	if limit <= 0 || limit > 50 {
		limit = 8
	}

	for _, pass := range []func(context.Context, string, int) ([]Hit, error){
		s.searchAllTerms,
		s.searchAnyTerm,
		s.searchTrigram,
	} {
		hits, err := pass(ctx, query, limit)
		if err != nil || len(hits) > 0 {
			return hits, err
		}
	}
	return []Hit{}, nil
}

// tsHeadline is shared by both full-text passes so a result looks the same
// however it was found.
const tsHeadline = `ts_headline($1, c.body, q.tsq,
	'StartSel=«, StopSel=», MaxFragments=2, MinWords=8, MaxWords=28')`

func (s *Store) searchAllTerms(ctx context.Context, query string, limit int) ([]Hit, error) {
	// plainto_tsquery rather than to_tsquery: the input is whatever a user
	// typed, and to_tsquery treats `&`, `|` and `!` as operators — a question
	// containing an ampersand would fail with a syntax error rather than a
	// search result.
	rows, err := s.db.Query(ctx, `
		WITH q AS (SELECT plainto_tsquery($1, $2) AS tsq)
		SELECT`+hitColumns+`, ts_rank_cd(c.tsv, q.tsq), `+tsHeadline+`
		  FROM wiki_chunks c
		  JOIN wiki_documents d ON d.id = c.document_id, q
		 WHERE c.tsv @@ q.tsq
		 ORDER BY ts_rank_cd(c.tsv, q.tsq) DESC, c.ordinal
		 LIMIT $3`, searchConfig, query, limit)
	if err != nil {
		return nil, err
	}
	return scanHits(rows, "all")
}

// searchAnyTerm ORs the query's terms together.
//
// The query is rebuilt from the *lexemes* the same stemmer produced, rather
// than from the raw words: they are already normalised and stop words are
// already gone, so what gets ORed is exactly what the index holds. Each is
// quoted, so a lexeme that happens to contain punctuation is a search term and
// never an operator — the injection this would otherwise open.
//
// ts_rank_cd does the discrimination the OR gave up: a passage matching three
// of four terms ranks well above one matching a single common word.
func (s *Store) searchAnyTerm(ctx context.Context, query string, limit int) ([]Hit, error) {
	rows, err := s.db.Query(ctx, `
		WITH q AS (
			SELECT to_tsquery($1, string_agg(quote_literal(lexeme), ' | ')) AS tsq
			  FROM unnest(to_tsvector($1, $2))
		)
		SELECT`+hitColumns+`, ts_rank_cd(c.tsv, q.tsq), `+tsHeadline+`
		  FROM wiki_chunks c
		  JOIN wiki_documents d ON d.id = c.document_id, q
		 WHERE q.tsq IS NOT NULL
		   AND c.tsv @@ q.tsq
		   AND ts_rank_cd(c.tsv, q.tsq) >= $4
		 ORDER BY ts_rank_cd(c.tsv, q.tsq) DESC, c.ordinal
		 LIMIT $3`, searchConfig, query, limit, minAnyRank)
	if err != nil {
		return nil, err
	}
	return scanHits(rows, "any")
}

// minAnyRank is the floor under the OR pass.
//
// Without it an OR query returns a passage that shares one incidental word with
// the question, and this is not a cosmetic problem: those passages are handed
// to the model as sources, so an unanswerable question comes back as a fluent
// answer built out of unrelated text. Measured on this corpus, "zeytinyağlı
// enginar tarifi" — nothing to do with anything indexed — returned two passages
// at rank 0.1 before this floor existed.
//
// 0.2 is read off how ts_rank_cd scores here rather than guessed: body lexemes
// carry the default 'D' weight of 0.1, so one incidental match scores about
// 0.1 and two, or one in a heading, clear 0.2. Real questions in the same
// measurement scored 0.4 to 0.9, so the floor separates the two populations
// with room to spare.
const minAnyRank = 0.2

func (s *Store) searchTrigram(ctx context.Context, query string, limit int) ([]Hit, error) {
	// word_similarity, not similarity, and the `%>` operator that goes with it.
	//
	// Plain similarity() divides by the union of both trigram sets, so a
	// five-word question compared against a 1200-character passage scores near
	// zero however well it matches — the passage's own trigrams swamp the
	// denominator. word_similarity scores the query against the best-matching
	// *extent* within the passage instead, which is the question actually being
	// asked. Using similarity() here would rank by length rather than by
	// relevance while looking like a real score.
	rows, err := s.db.Query(ctx, `
		SELECT`+hitColumns+`,
		       word_similarity($1, c.body),
		       left(c.body, 240)
		  FROM wiki_chunks c
		  JOIN wiki_documents d ON d.id = c.document_id
		 WHERE c.body %> $1
		 ORDER BY word_similarity($1, c.body) DESC, c.ordinal
		 LIMIT $2`, query, limit)
	if err != nil {
		return nil, err
	}
	return scanHits(rows, "fuzzy")
}

func scanHits(rows pgx.Rows, matched string) ([]Hit, error) {
	defer rows.Close()
	out := []Hit{}
	for rows.Next() {
		var h Hit
		if err := rows.Scan(&h.DocumentSlug, &h.Title, &h.SourceURL, &h.Ordinal,
			&h.Heading, &h.Body, &h.Rank, &h.Snippet); err != nil {
			return nil, err
		}
		h.Matched = matched
		out = append(out, h)
	}
	return out, rows.Err()
}

// nullIfEmpty keeps an empty user id out of a uuid column. Ingestion can be
// driven by a script with no session, and ” is not a uuid.
func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
