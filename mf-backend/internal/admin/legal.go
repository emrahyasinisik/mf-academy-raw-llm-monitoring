package admin

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/emrah/mf-backend/internal/common"
	"github.com/go-chi/chi/v5"
)

// LegalStore is the legal-document surface behind /admin/legal and the public
// reader used by GET /legal/{slug}.
type LegalStore interface {
	GetPublishedLegal(ctx context.Context, slug string) (LegalDocument, error)
	RequiredTermsVersion(ctx context.Context) (string, error)
	ListLegalSummaries(ctx context.Context) ([]LegalListItem, error)
	GetLegalSlug(ctx context.Context, slug string) (LegalSlugDetail, error)
	SaveLegalDraft(ctx context.Context, slug, title, body string) (LegalDocument, error)
	PublishLegalDraft(ctx context.Context, slug string, requiresReconsent bool, publisherID string) (LegalDocument, error)
	DeleteLegalDraft(ctx context.Context, slug string) error
}

type saveLegalDraftRequest struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

type publishLegalRequest struct {
	RequiresReconsent bool `json:"requires_reconsent"`
}

func validLegalSlug(slug string) bool {
	switch slug {
	case "gizlilik", "kosullar":
		return true
	default:
		return false
	}
}

// PublicLegal serves GET /legal/{slug} without auth.
func (h *Handler) PublicLegal(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if !validLegalSlug(slug) {
		common.Error(w, common.ErrNotFound("document not found"))
		return
	}
	doc, err := h.legal.GetPublishedLegal(r.Context(), slug)
	if errors.Is(err, errLegalNotFound) {
		common.Error(w, common.ErrNotFound("document not found"))
		return
	}
	if err != nil {
		common.Error(w, common.ErrInternal("could not load document"))
		return
	}
	common.JSON(w, http.StatusOK, doc)
}

// ListLegal GET /admin/legal
func (h *Handler) ListLegal(w http.ResponseWriter, r *http.Request) {
	items, err := h.legal.ListLegalSummaries(r.Context())
	if err != nil {
		common.Error(w, common.ErrInternal("could not list documents"))
		return
	}
	common.JSON(w, http.StatusOK, map[string]any{"documents": items})
}

// GetLegal GET /admin/legal/{slug}
func (h *Handler) GetLegal(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if !validLegalSlug(slug) {
		common.Error(w, common.ErrNotFound("document not found"))
		return
	}
	detail, err := h.legal.GetLegalSlug(r.Context(), slug)
	if err != nil {
		common.Error(w, common.ErrInternal("could not load document"))
		return
	}
	common.JSON(w, http.StatusOK, detail)
}

// SaveLegalDraft PUT /admin/legal/{slug}
func (h *Handler) SaveLegalDraft(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if !validLegalSlug(slug) {
		common.Error(w, common.ErrNotFound("document not found"))
		return
	}
	var req saveLegalDraftRequest
	if err := common.Decode(r, &req); err != nil {
		common.Error(w, err)
		return
	}
	title := strings.TrimSpace(req.Title)
	body := strings.TrimSpace(req.Body)
	if title == "" || body == "" {
		common.Error(w, common.ErrBadRequest("title and body are required"))
		return
	}
	doc, err := h.legal.SaveLegalDraft(r.Context(), slug, title, body)
	if err != nil {
		common.Error(w, common.ErrInternal("could not save draft"))
		return
	}
	common.JSON(w, http.StatusOK, doc)
}

// PublishLegal POST /admin/legal/{slug}/publish
func (h *Handler) PublishLegal(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if !validLegalSlug(slug) {
		common.Error(w, common.ErrNotFound("document not found"))
		return
	}
	var req publishLegalRequest
	if err := common.Decode(r, &req); err != nil {
		common.Error(w, err)
		return
	}
	claims, _ := common.ClaimsFromContext(r.Context())
	doc, err := h.legal.PublishLegalDraft(r.Context(), slug, req.RequiresReconsent, claims.UserID)
	if errors.Is(err, errLegalNoDraft) {
		common.Error(w, common.ErrBadRequest("no draft to publish"))
		return
	}
	if err != nil {
		common.Error(w, common.ErrInternal("could not publish"))
		return
	}
	h.recordAudit(r.Context(), claims.UserID, "legal.publish", slug, map[string]any{
		"version":            doc.Version,
		"requires_reconsent": doc.RequiresReconsent,
	})
	common.JSON(w, http.StatusOK, doc)
}

// DeleteLegalDraft DELETE /admin/legal/{slug}/draft
func (h *Handler) DeleteLegalDraft(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if !validLegalSlug(slug) {
		common.Error(w, common.ErrNotFound("document not found"))
		return
	}
	if err := h.legal.DeleteLegalDraft(r.Context(), slug); errors.Is(err, errLegalNotFound) {
		common.Error(w, common.ErrNotFound("draft not found"))
		return
	} else if err != nil {
		common.Error(w, common.ErrInternal("could not delete draft"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
