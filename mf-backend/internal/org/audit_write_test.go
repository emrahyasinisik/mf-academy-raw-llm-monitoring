package org

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/emrah/mf-backend/internal/common"
	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/bcrypt"
)

type recordingAudit struct {
	calls []auditCall
}

type auditCall struct {
	actorID string
	action  string
	target  string
	detail  map[string]any
}

func (r *recordingAudit) WriteAudit(_ context.Context, actorID, action, target string, detail map[string]any) error {
	r.calls = append(r.calls, auditCall{actorID: actorID, action: action, target: target, detail: detail})
	return nil
}

func orgAdminHandler(store OrgStore, audit AuditWriter) *Handler {
	return NewHandler(store, audit, bcrypt.MinCost)
}

func TestCreateMemberWritesAudit(t *testing.T) {
	store := &fakeOrgStore{orgs: map[string]OrgSummary{
		"org-a": {ID: "org-a", Name: "Acme", Type: "company", SeatLimit: 5, Status: "active", MemberCount: 1},
	}}
	audit := &recordingAudit{}
	h := orgAdminHandler(store, audit)
	rtr := h.Routes(claimsVerifier(common.AuthClaims{
		UserID: "actor-1", OrgID: "org-a", OrgRole: "admin", OrgType: "company",
	}), 0)

	body := bytes.NewBufferString(`{"name":"Ada","email":"ada@example.com","org_role":"admin"}`)
	req := httptest.NewRequest(http.MethodPost, "/members", body)
	req.Header.Set("Authorization", "Bearer t")
	w := httptest.NewRecorder()
	rtr.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", w.Code, w.Body.String())
	}
	if len(audit.calls) != 1 {
		t.Fatalf("audit calls = %d, want 1", len(audit.calls))
	}
	c := audit.calls[0]
	if c.actorID != "actor-1" || c.action != "org.member.create" || c.target != "new-user" {
		t.Fatalf("audit call = %+v, want actor-1 / org.member.create / new-user", c)
	}
	if c.detail["org_role"] != "admin" {
		t.Fatalf("detail org_role = %v, want admin", c.detail["org_role"])
	}
	if _, ok := c.detail["email"]; ok {
		t.Fatal("audit detail must not contain email")
	}
}

func TestSetMemberRoleWritesAudit(t *testing.T) {
	store := seededMemberStore()
	audit := &recordingAudit{}
	h := orgAdminHandler(store, audit)
	rtr := h.Routes(claimsVerifier(common.AuthClaims{
		UserID: "actor-1", OrgID: "org-a", OrgRole: "admin", OrgType: "company",
	}), 0)

	body := bytes.NewBufferString(`{"org_role":"member"}`)
	req := httptest.NewRequest(http.MethodPatch, "/members/admin-a", body)
	req.Header.Set("Authorization", "Bearer t")
	w := httptest.NewRecorder()
	rtr.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if len(audit.calls) != 1 {
		t.Fatalf("audit calls = %d, want 1", len(audit.calls))
	}
	c := audit.calls[0]
	if c.actorID != "actor-1" || c.action != "org.member.role" || c.target != "admin-a" {
		t.Fatalf("audit call = %+v", c)
	}
	if c.detail["org_role"] != "member" {
		t.Fatalf("detail org_role = %v, want member", c.detail["org_role"])
	}
}

func TestDeleteMemberWritesAudit(t *testing.T) {
	store := seededMemberStore()
	audit := &recordingAudit{}
	h := orgAdminHandler(store, audit)
	rtr := h.Routes(claimsVerifier(common.AuthClaims{
		UserID: "actor-1", OrgID: "org-a", OrgRole: "admin", OrgType: "company",
	}), 0)

	req := httptest.NewRequest(http.MethodDelete, "/members/member-a", nil)
	req.Header.Set("Authorization", "Bearer t")
	w := httptest.NewRecorder()
	rtr.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body=%s", w.Code, w.Body.String())
	}
	if len(audit.calls) != 1 {
		t.Fatalf("audit calls = %d, want 1", len(audit.calls))
	}
	c := audit.calls[0]
	if c.actorID != "actor-1" || c.action != "org.member.remove" || c.target != "member-a" {
		t.Fatalf("audit call = %+v", c)
	}
	if c.detail["org_role"] != "member" {
		t.Fatalf("detail org_role = %v, want member", c.detail["org_role"])
	}
}

func TestMemberMutationSkippedOnFailure(t *testing.T) {
	store := &fakeOrgStore{orgs: map[string]OrgSummary{
		"org-a": {ID: "org-a", Name: "Acme", Type: "company", SeatLimit: 3, Status: "active", MemberCount: 3},
	}}
	audit := &recordingAudit{}
	h := orgAdminHandler(store, audit)
	rtr := h.Routes(claimsVerifier(common.AuthClaims{
		UserID: "actor-1", OrgID: "org-a", OrgRole: "admin", OrgType: "company",
	}), 0)

	body := bytes.NewBufferString(`{"name":"Bob","email":"bob@example.com","org_role":"admin"}`)
	req := httptest.NewRequest(http.MethodPost, "/members", body)
	req.Header.Set("Authorization", "Bearer t")
	w := httptest.NewRecorder()
	rtr.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", w.Code)
	}
	if len(audit.calls) != 0 {
		t.Fatalf("audit must not run on failed create; calls = %+v", audit.calls)
	}

	req = httptest.NewRequest(http.MethodPatch, "/members/owner-a", bytes.NewBufferString(`{"org_role":"member"}`))
	req.Header.Set("Authorization", "Bearer t")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "owner-a")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w = httptest.NewRecorder()
	h.SetMemberRole(w, req.WithContext(common.ContextWithClaims(req.Context(), common.AuthClaims{
		UserID: "actor-1", OrgID: "org-a", OrgRole: "admin", OrgType: "company",
	})))
	if len(audit.calls) != 0 {
		t.Fatalf("audit must not run on owner patch failure; calls = %+v", audit.calls)
	}
}
