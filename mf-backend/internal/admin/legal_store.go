package admin

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// LegalDocument is one row of legal_documents — draft or published.
type LegalDocument struct {
	ID                string     `json:"id"`
	Slug              string     `json:"slug"`
	Title             string     `json:"title"`
	Version           string     `json:"version"`
	Body              string     `json:"body"`
	RequiresReconsent bool       `json:"requires_reconsent"`
	IsDraft           bool       `json:"is_draft"`
	PublishedAt       *time.Time `json:"published_at,omitempty"`
	PublishedBy       *string    `json:"published_by,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
}

// LegalSlugDetail is the admin view for one slug: open draft + publish history.
type LegalSlugDetail struct {
	Slug     string          `json:"slug"`
	Draft    *LegalDocument  `json:"draft"`
	History  []LegalDocument `json:"history"`
}

// LegalListItem is one row in GET /admin/legal.
type LegalListItem struct {
	Slug              string     `json:"slug"`
	Title             string     `json:"title"`
	Version           string     `json:"version"`
	HasDraft          bool       `json:"has_draft"`
	PublishedAt       *time.Time `json:"published_at,omitempty"`
	RequiresReconsent bool       `json:"requires_reconsent"`
}

var (
	errLegalNotFound = errors.New("legal document not found")
	errLegalNoDraft  = errors.New("no draft to publish")
)

func scanLegalDoc(row pgx.Row) (LegalDocument, error) {
	var d LegalDocument
	err := row.Scan(
		&d.ID, &d.Slug, &d.Title, &d.Version, &d.Body,
		&d.RequiresReconsent, &d.IsDraft, &d.PublishedAt, &d.PublishedBy, &d.CreatedAt,
	)
	return d, err
}

// GetPublishedLegal returns the latest published document for a slug.
func (s *Store) GetPublishedLegal(ctx context.Context, slug string) (LegalDocument, error) {
	d, err := scanLegalDoc(s.db.QueryRow(ctx, `
		SELECT id, slug, title, version, body, requires_reconsent, is_draft,
		       published_at, published_by, created_at
		  FROM legal_documents
		 WHERE slug = $1 AND is_draft = false
		 ORDER BY published_at DESC
		 LIMIT 1`, slug))
	if errors.Is(err, pgx.ErrNoRows) {
		return LegalDocument{}, errLegalNotFound
	}
	return d, err
}

// RequiredTermsVersion is the version the consent gate compares against:
// latest published kosullar row. Empty string if none (gate then keys only on
// acceptance having happened — should not occur after seed).
func (s *Store) RequiredTermsVersion(ctx context.Context) (string, error) {
	var version string
	err := s.db.QueryRow(ctx, `
		SELECT version FROM legal_documents
		 WHERE slug = 'kosullar' AND is_draft = false
		 ORDER BY published_at DESC
		 LIMIT 1`).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return version, err
}

// ListLegalSummaries returns one summary per known slug.
func (s *Store) ListLegalSummaries(ctx context.Context) ([]LegalListItem, error) {
	rows, err := s.db.Query(ctx, `
		SELECT s.slug,
		       COALESCE(p.title, s.slug) AS title,
		       COALESCE(p.version, '') AS version,
		       EXISTS (
		         SELECT 1 FROM legal_documents d
		          WHERE d.slug = s.slug AND d.is_draft = true
		       ) AS has_draft,
		       p.published_at,
		       COALESCE(p.requires_reconsent, false) AS requires_reconsent
		  FROM (VALUES ('gizlilik'), ('kosullar')) AS s(slug)
		  LEFT JOIN LATERAL (
		    SELECT title, version, published_at, requires_reconsent
		      FROM legal_documents
		     WHERE slug = s.slug AND is_draft = false
		     ORDER BY published_at DESC
		     LIMIT 1
		  ) p ON true
		 ORDER BY s.slug`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []LegalListItem{}
	for rows.Next() {
		var it LegalListItem
		if err := rows.Scan(&it.Slug, &it.Title, &it.Version, &it.HasDraft, &it.PublishedAt, &it.RequiresReconsent); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// GetLegalSlug returns open draft + published history for a slug.
func (s *Store) GetLegalSlug(ctx context.Context, slug string) (LegalSlugDetail, error) {
	detail := LegalSlugDetail{Slug: slug, History: []LegalDocument{}}

	draft, err := scanLegalDoc(s.db.QueryRow(ctx, `
		SELECT id, slug, title, version, body, requires_reconsent, is_draft,
		       published_at, published_by, created_at
		  FROM legal_documents
		 WHERE slug = $1 AND is_draft = true
		 ORDER BY created_at DESC
		 LIMIT 1`, slug))
	if err == nil {
		detail.Draft = &draft
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return LegalSlugDetail{}, err
	}

	rows, err := s.db.Query(ctx, `
		SELECT id, slug, title, version, body, requires_reconsent, is_draft,
		       published_at, published_by, created_at
		  FROM legal_documents
		 WHERE slug = $1 AND is_draft = false
		 ORDER BY published_at DESC`, slug)
	if err != nil {
		return LegalSlugDetail{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var d LegalDocument
		if err := rows.Scan(
			&d.ID, &d.Slug, &d.Title, &d.Version, &d.Body,
			&d.RequiresReconsent, &d.IsDraft, &d.PublishedAt, &d.PublishedBy, &d.CreatedAt,
		); err != nil {
			return LegalSlugDetail{}, err
		}
		detail.History = append(detail.History, d)
	}
	return detail, rows.Err()
}

// SaveLegalDraft upserts the single open draft for a slug.
func (s *Store) SaveLegalDraft(ctx context.Context, slug, title, body string) (LegalDocument, error) {
	var id string
	err := s.db.QueryRow(ctx, `
		SELECT id FROM legal_documents WHERE slug = $1 AND is_draft = true LIMIT 1`, slug).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return scanLegalDoc(s.db.QueryRow(ctx, `
			INSERT INTO legal_documents (slug, title, version, body, is_draft)
			VALUES ($1, $2, '', $3, true)
			RETURNING id, slug, title, version, body, requires_reconsent, is_draft,
			          published_at, published_by, created_at`, slug, title, body))
	}
	if err != nil {
		return LegalDocument{}, err
	}
	return scanLegalDoc(s.db.QueryRow(ctx, `
		UPDATE legal_documents SET title = $2, body = $3
		 WHERE id = $1
		 RETURNING id, slug, title, version, body, requires_reconsent, is_draft,
		           published_at, published_by, created_at`, id, title, body))
}

// PublishLegalDraft turns the open draft into a published row (append-only).
// The draft row is removed after a published copy is inserted.
func (s *Store) PublishLegalDraft(ctx context.Context, slug string, requiresReconsent bool, publisherID string) (LegalDocument, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return LegalDocument{}, err
	}
	defer tx.Rollback(ctx)

	draft, err := scanLegalDoc(tx.QueryRow(ctx, `
		SELECT id, slug, title, version, body, requires_reconsent, is_draft,
		       published_at, published_by, created_at
		  FROM legal_documents
		 WHERE slug = $1 AND is_draft = true
		 LIMIT 1
		 FOR UPDATE`, slug))
	if errors.Is(err, pgx.ErrNoRows) {
		return LegalDocument{}, errLegalNoDraft
	}
	if err != nil {
		return LegalDocument{}, err
	}

	version := draft.Version
	if requiresReconsent {
		version, err = nextLegalVersion(ctx, tx, slug, time.Now().UTC())
		if err != nil {
			return LegalDocument{}, err
		}
	} else {
		var prev string
		err = tx.QueryRow(ctx, `
			SELECT version FROM legal_documents
			 WHERE slug = $1 AND is_draft = false
			 ORDER BY published_at DESC LIMIT 1`, slug).Scan(&prev)
		if err == nil {
			version = prev
		} else if errors.Is(err, pgx.ErrNoRows) {
			if version == "" {
				version = time.Now().UTC().Format("2006-01-02")
			}
		} else {
			return LegalDocument{}, err
		}
	}

	pub, err := scanLegalDoc(tx.QueryRow(ctx, `
		INSERT INTO legal_documents
		  (slug, title, version, body, requires_reconsent, is_draft, published_at, published_by)
		VALUES ($1, $2, $3, $4, $5, false, now(), $6)
		RETURNING id, slug, title, version, body, requires_reconsent, is_draft,
		          published_at, published_by, created_at`,
		slug, draft.Title, version, draft.Body, requiresReconsent, publisherID))
	if err != nil {
		return LegalDocument{}, err
	}

	if _, err := tx.Exec(ctx, `DELETE FROM legal_documents WHERE id = $1`, draft.ID); err != nil {
		return LegalDocument{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return LegalDocument{}, err
	}
	return pub, nil
}

func nextLegalVersion(ctx context.Context, tx pgx.Tx, slug string, now time.Time) (string, error) {
	base := now.Format("2006-01-02")
	var count int
	err := tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM legal_documents
		 WHERE slug = $1 AND is_draft = false AND version LIKE $2`,
		slug, base+"%").Scan(&count)
	if err != nil {
		return "", err
	}
	if count == 0 {
		return base, nil
	}
	return fmt.Sprintf("%s-%d", base, count+1), nil
}

// DeleteLegalDraft removes the open draft for a slug.
func (s *Store) DeleteLegalDraft(ctx context.Context, slug string) error {
	tag, err := s.db.Exec(ctx, `
		DELETE FROM legal_documents WHERE slug = $1 AND is_draft = true`, slug)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errLegalNotFound
	}
	return nil
}
