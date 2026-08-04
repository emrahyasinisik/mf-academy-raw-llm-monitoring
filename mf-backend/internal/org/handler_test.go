package org

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/emrah/mf-backend/internal/common"
)

type fakeOrgStore struct {
	orgs   map[string]OrgSummary
	lastID string
}

func (f *fakeOrgStore) GetOrgSummary(_ context.Context, orgID string) (OrgSummary, error) {
	f.lastID = orgID
	s, ok := f.orgs[orgID]
	if !ok {
		return OrgSummary{}, ErrNoRows
	}
	return s, nil
}

func claimsVerifier(c common.AuthClaims) common.TokenVerifier {
	return func(string) (common.AuthClaims, error) {
		return c, nil
	}
}

func TestOrgMeRequiresOrgAdmin(t *testing.T) {
	store := &fakeOrgStore{orgs: map[string]OrgSummary{
		"org-a": {ID: "org-a", Name: "A", Type: "company", SeatLimit: 5, Status: "active", MemberCount: 2},
	}}
	h := NewHandler(store)
	rtr := h.Routes(claimsVerifier(common.AuthClaims{
		UserID: "u1", OrgID: "org-a", OrgRole: "member", OrgType: "company",
	}), time.Second)

	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set("Authorization", "Bearer t")
	w := httptest.NewRecorder()
	rtr.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", w.Code, w.Body.String())
	}
	if store.lastID != "" {
		t.Fatal("store must not be queried when role is member")
	}
}

func TestOrgMeReturnsActorOrgOnly(t *testing.T) {
	store := &fakeOrgStore{orgs: map[string]OrgSummary{
		"org-a": {ID: "org-a", Name: "Acme", Type: "company", SeatLimit: 5, Status: "active", MemberCount: 3},
		"org-b": {ID: "org-b", Name: "Beta", Type: "company", SeatLimit: 10, Status: "active", MemberCount: 8},
	}}
	h := NewHandler(store)
	rtr := h.Routes(claimsVerifier(common.AuthClaims{
		UserID: "u1", OrgID: "org-a", OrgRole: "owner", OrgType: "company",
	}), time.Second)

	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set("Authorization", "Bearer t")
	w := httptest.NewRecorder()
	rtr.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var res MeResponse
	if err := json.NewDecoder(w.Body).Decode(&res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res.Org.ID != "org-a" {
		t.Fatalf("org.id = %q, want org-a", res.Org.ID)
	}
	if res.Org.Name != "Acme" || res.Org.SeatLimit != 5 || res.Org.MemberCount != 3 {
		t.Fatalf("org summary = %+v, want Acme / seat 5 / members 3", res.Org)
	}
	if store.lastID != "org-a" {
		t.Fatalf("store queried %q, want org-a", store.lastID)
	}
	if res.Role != "owner" {
		t.Fatalf("role = %q, want owner", res.Role)
	}
}

func TestOrgMePasswordResetBlocked(t *testing.T) {
	store := &fakeOrgStore{orgs: map[string]OrgSummary{
		"org-a": {ID: "org-a", Name: "Acme", Type: "company", SeatLimit: 5, Status: "active", MemberCount: 1},
	}}
	h := NewHandler(store)
	rtr := h.Routes(claimsVerifier(common.AuthClaims{
		UserID: "u1", OrgID: "org-a", OrgRole: "admin", OrgType: "company", PasswordReset: true,
	}), time.Second)

	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set("Authorization", "Bearer t")
	w := httptest.NewRecorder()
	rtr.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", w.Code, w.Body.String())
	}
	var body common.ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error != "password_change_required" {
		t.Fatalf("error = %q, want password_change_required", body.Error)
	}
	if store.lastID != "" {
		t.Fatal("store must not be queried when password reset is required")
	}
}
