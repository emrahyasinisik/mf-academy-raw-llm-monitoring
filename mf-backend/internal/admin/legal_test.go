package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/emrah/mf-backend/internal/common"
	"github.com/go-chi/chi/v5"
)

type fakeLegalStore struct {
	published   map[string]LegalDocument
	drafts      map[string]LegalDocument
	history     map[string][]LegalDocument
	lastPublish *LegalDocument
}

func (f *fakeLegalStore) GetPublishedLegal(_ context.Context, slug string) (LegalDocument, error) {
	d, ok := f.published[slug]
	if !ok {
		return LegalDocument{}, errLegalNotFound
	}
	return d, nil
}

func (f *fakeLegalStore) RequiredTermsVersion(context.Context) (string, error) {
	if d, ok := f.published["kosullar"]; ok {
		return d.Version, nil
	}
	return "", nil
}

func (f *fakeLegalStore) ListLegalSummaries(context.Context) ([]LegalListItem, error) {
	return []LegalListItem{{Slug: "kosullar", Title: "Kullanım koşulları", Version: "2026-08-01"}}, nil
}

func (f *fakeLegalStore) GetLegalSlug(_ context.Context, slug string) (LegalSlugDetail, error) {
	d := LegalSlugDetail{Slug: slug, History: f.history[slug]}
	if draft, ok := f.drafts[slug]; ok {
		cp := draft
		d.Draft = &cp
	}
	if d.History == nil {
		d.History = []LegalDocument{}
	}
	return d, nil
}

func (f *fakeLegalStore) SaveLegalDraft(_ context.Context, slug, title, body string) (LegalDocument, error) {
	d := LegalDocument{ID: "draft-1", Slug: slug, Title: title, Body: body, IsDraft: true, CreatedAt: time.Unix(1, 0)}
	if f.drafts == nil {
		f.drafts = map[string]LegalDocument{}
	}
	f.drafts[slug] = d
	return d, nil
}

func (f *fakeLegalStore) PublishLegalDraft(_ context.Context, slug string, requiresReconsent bool, publisherID string) (LegalDocument, error) {
	draft, ok := f.drafts[slug]
	if !ok {
		return LegalDocument{}, errLegalNoDraft
	}
	version := "2026-08-01"
	if pub, ok := f.published[slug]; ok {
		version = pub.Version
	}
	if requiresReconsent {
		version = "2026-08-04"
	}
	now := time.Unix(10, 0)
	pub := LegalDocument{
		ID: "pub-1", Slug: slug, Title: draft.Title, Version: version, Body: draft.Body,
		RequiresReconsent: requiresReconsent, IsDraft: false, PublishedAt: &now, PublishedBy: &publisherID,
		CreatedAt: now,
	}
	if f.published == nil {
		f.published = map[string]LegalDocument{}
	}
	f.published[slug] = pub
	delete(f.drafts, slug)
	f.lastPublish = &pub
	return pub, nil
}

func (f *fakeLegalStore) DeleteLegalDraft(_ context.Context, slug string) error {
	if _, ok := f.drafts[slug]; !ok {
		return errLegalNotFound
	}
	delete(f.drafts, slug)
	return nil
}

func legalRouter(h *Handler) http.Handler {
	r := chi.NewRouter()
	r.Get("/legal/{slug}", h.PublicLegal)
	r.Route("/admin/legal", func(lr chi.Router) {
		lr.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ctx := common.ContextWithClaims(r.Context(), common.AuthClaims{UserID: "admin-1", Role: common.RoleAdmin})
				next.ServeHTTP(w, r.WithContext(ctx))
			})
		})
		lr.Get("/", h.ListLegal)
		lr.Get("/{slug}", h.GetLegal)
		lr.Put("/{slug}", h.SaveLegalDraft)
		lr.Post("/{slug}/publish", h.PublishLegal)
		lr.Delete("/{slug}/draft", h.DeleteLegalDraft)
	})
	return r
}

func TestPublicLegalHidesMissingAndReturnsPublished(t *testing.T) {
	now := time.Unix(5, 0)
	store := &fakeLegalStore{published: map[string]LegalDocument{
		"gizlilik": {Slug: "gizlilik", Title: "Gizlilik", Version: "2026-08-01", Body: "body", PublishedAt: &now},
	}}
	h := &Handler{legal: store}
	r := legalRouter(h)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/legal/gizlilik", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", w.Code)
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/legal/kosullar", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing published status %d, want 404", w.Code)
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/legal/draft-only", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("unknown slug status %d, want 404", w.Code)
	}
}

func TestPublishKeepsVersionUnlessReconsent(t *testing.T) {
	now := time.Unix(5, 0)
	store := &fakeLegalStore{
		published: map[string]LegalDocument{
			"kosullar": {Slug: "kosullar", Title: "Koşullar", Version: "2026-08-01", Body: "old", PublishedAt: &now},
		},
		drafts: map[string]LegalDocument{
			"kosullar": {Slug: "kosullar", Title: "Koşullar", Body: "new draft", IsDraft: true},
		},
	}
	h := &Handler{legal: store}
	r := legalRouter(h)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/admin/legal/kosullar/publish", strings.NewReader(`{"requires_reconsent":false}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	var kept LegalDocument
	if err := json.Unmarshal(w.Body.Bytes(), &kept); err != nil {
		t.Fatal(err)
	}
	if kept.Version != "2026-08-01" || kept.RequiresReconsent {
		t.Fatalf("kept publish = %+v, want version 2026-08-01 without reconsent", kept)
	}

	store.drafts["kosullar"] = LegalDocument{Slug: "kosullar", Title: "Koşullar", Body: "bump", IsDraft: true}
	w = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/admin/legal/kosullar/publish", strings.NewReader(`{"requires_reconsent":true}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("reconsent status %d body %s", w.Code, w.Body.String())
	}
	var bumped LegalDocument
	if err := json.Unmarshal(w.Body.Bytes(), &bumped); err != nil {
		t.Fatal(err)
	}
	if bumped.Version != "2026-08-04" || !bumped.RequiresReconsent {
		t.Fatalf("reconsent publish = %+v", bumped)
	}
}

func TestPublishWithoutDraftIsBadRequest(t *testing.T) {
	h := &Handler{legal: &fakeLegalStore{}}
	r := legalRouter(h)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/admin/legal/kosullar/publish", strings.NewReader(`{"requires_reconsent":false}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", w.Code)
	}
}
