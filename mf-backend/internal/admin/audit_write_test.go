package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/emrah/mf-backend/internal/common"
	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/bcrypt"
)

type recordingAudit struct {
	actions []string
}

func (r *recordingAudit) WriteAudit(_ context.Context, _, action, _ string, _ map[string]any) error {
	r.actions = append(r.actions, action)
	return nil
}

func (r *recordingAudit) ListAudit(context.Context, int, int) (AuditListResult, error) {
	return AuditListResult{Entries: []AuditEntry{}}, nil
}

func TestCreateAccountWritesAudit(t *testing.T) {
	accounts := &fakeAccountStore{}
	audit := &recordingAudit{}
	h := &Handler{accounts: accounts, audit: audit, bcryptCost: bcrypt.MinCost}
	r := chi.NewRouter()
	r.Post("/accounts", func(w http.ResponseWriter, req *http.Request) {
		ctx := common.ContextWithClaims(req.Context(), common.AuthClaims{UserID: "admin-1"})
		h.CreateAccount(w, req.WithContext(ctx))
	})

	body := `{"type":"individual","name":"Ada","email":"ada@example.com"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/accounts", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	if len(audit.actions) != 1 || audit.actions[0] != "account.create" {
		t.Fatalf("audit actions = %v", audit.actions)
	}
}

func TestPublishLegalWritesAudit(t *testing.T) {
	legal := &fakeLegalStore{
		drafts: map[string]LegalDocument{
			"kosullar": {Slug: "kosullar", Title: "K", Body: "b", IsDraft: true},
		},
	}
	audit := &recordingAudit{}
	h := &Handler{legal: legal, audit: audit}
	r := chi.NewRouter()
	r.Post("/legal/{slug}/publish", h.PublishLegal)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/legal/kosullar/publish", strings.NewReader(`{"requires_reconsent":false}`))
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("slug", "kosullar")
	ctx := common.ContextWithClaims(context.WithValue(req.Context(), chi.RouteCtxKey, rctx), common.AuthClaims{UserID: "admin-1"})
	r.ServeHTTP(w, req.WithContext(ctx))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	if len(audit.actions) != 1 || audit.actions[0] != "legal.publish" {
		t.Fatalf("audit actions = %v", audit.actions)
	}
}
