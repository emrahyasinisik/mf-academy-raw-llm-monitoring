package org

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/emrah/mf-backend/internal/common"
	"golang.org/x/crypto/bcrypt"
)

type fakeOrgStore struct {
	orgs    map[string]OrgSummary
	members map[string][]Member

	lastID            string
	createdOrgID      string
	createdEmail      string
	createdRole       string
	storedHash        string
	createdMustChange bool
	createCalled      bool
}

func (f *fakeOrgStore) GetOrgSummary(_ context.Context, orgID string) (OrgSummary, error) {
	f.lastID = orgID
	s, ok := f.orgs[orgID]
	if !ok {
		return OrgSummary{}, ErrNoRows
	}
	return s, nil
}

func (f *fakeOrgStore) ListMembers(_ context.Context, orgID string) ([]Member, error) {
	f.lastID = orgID
	if f.members == nil {
		return []Member{}, nil
	}
	out := f.members[orgID]
	if out == nil {
		return []Member{}, nil
	}
	return out, nil
}

func (f *fakeOrgStore) CreateMember(_ context.Context, orgID, name, email, orgRole, passwordHash string) (Member, error) {
	f.createCalled = true
	f.createdOrgID = orgID
	f.createdEmail = email
	f.createdRole = orgRole
	f.storedHash = passwordHash
	f.createdMustChange = true
	m := Member{
		ID:        "new-user",
		Email:     email,
		Name:      name,
		OrgRole:   orgRole,
		CreatedAt: time.Unix(1, 0).UTC(),
	}
	if f.members == nil {
		f.members = map[string][]Member{}
	}
	f.members[orgID] = append(f.members[orgID], m)
	if s, ok := f.orgs[orgID]; ok {
		s.MemberCount++
		f.orgs[orgID] = s
	}
	return m, nil
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
	h := NewHandler(store, bcrypt.MinCost)
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
	h := NewHandler(store, bcrypt.MinCost)
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
	h := NewHandler(store, bcrypt.MinCost)
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

func orgAdminRouter(store OrgStore) http.Handler {
	h := NewHandler(store, bcrypt.MinCost)
	return h.Routes(claimsVerifier(common.AuthClaims{
		UserID: "u1", OrgID: "org-a", OrgRole: "admin", OrgType: "company",
	}), time.Second)
}

func TestCreateMemberBelowSeat(t *testing.T) {
	store := &fakeOrgStore{orgs: map[string]OrgSummary{
		"org-a": {ID: "org-a", Name: "Acme", Type: "company", SeatLimit: 5, Status: "active", MemberCount: 2},
	}}
	rtr := orgAdminRouter(store)

	body := bytes.NewBufferString(`{"name":"Ada Lovelace","email":"ADA@example.com","org_role":"member"}`)
	req := httptest.NewRequest(http.MethodPost, "/members", body)
	req.Header.Set("Authorization", "Bearer t")
	w := httptest.NewRecorder()
	rtr.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", w.Code, w.Body.String())
	}
	var res CreateMemberResponse
	if err := json.NewDecoder(w.Body).Decode(&res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res.TemporaryPassword == "" {
		t.Fatal("temporary_password must be non-empty")
	}
	if res.Member.Email != "ada@example.com" {
		t.Fatalf("email = %q, want ada@example.com", res.Member.Email)
	}
	if res.Member.OrgRole != "member" {
		t.Fatalf("org_role = %q, want member", res.Member.OrgRole)
	}
	if !store.createdMustChange {
		t.Fatal("store must create with must_change_password=true")
	}
	if store.createdOrgID != "org-a" {
		t.Fatalf("created org_id = %q, want org-a", store.createdOrgID)
	}
	if store.storedHash == "" || store.storedHash == res.TemporaryPassword {
		t.Fatalf("store must receive bcrypt hash, got %q", store.storedHash)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(store.storedHash), []byte(res.TemporaryPassword)); err != nil {
		t.Fatalf("stored hash does not match temporary password: %v", err)
	}
}

func TestCreateMemberAtSeatLimit(t *testing.T) {
	store := &fakeOrgStore{orgs: map[string]OrgSummary{
		"org-a": {ID: "org-a", Name: "Acme", Type: "company", SeatLimit: 3, Status: "active", MemberCount: 3},
	}}
	rtr := orgAdminRouter(store)

	body := bytes.NewBufferString(`{"name":"Bob","email":"bob@example.com","org_role":"admin"}`)
	req := httptest.NewRequest(http.MethodPost, "/members", body)
	req.Header.Set("Authorization", "Bearer t")
	w := httptest.NewRecorder()
	rtr.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", w.Code, w.Body.String())
	}
	if store.createCalled {
		t.Fatal("CreateMember must not run when seat_limit is reached")
	}
}

func TestCreateMemberRejectsOwnerRole(t *testing.T) {
	store := &fakeOrgStore{orgs: map[string]OrgSummary{
		"org-a": {ID: "org-a", Name: "Acme", Type: "company", SeatLimit: 5, Status: "active", MemberCount: 1},
	}}
	rtr := orgAdminRouter(store)

	body := bytes.NewBufferString(`{"name":"Eve","email":"eve@example.com","org_role":"owner"}`)
	req := httptest.NewRequest(http.MethodPost, "/members", body)
	req.Header.Set("Authorization", "Bearer t")
	w := httptest.NewRecorder()
	rtr.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
	if store.createCalled {
		t.Fatal("CreateMember must not run for owner role")
	}
}

func TestListMembersScopedToActorOrg(t *testing.T) {
	store := &fakeOrgStore{
		orgs: map[string]OrgSummary{
			"org-a": {ID: "org-a", Name: "Acme", Type: "company", SeatLimit: 5, Status: "active", MemberCount: 2},
			"org-b": {ID: "org-b", Name: "Beta", Type: "company", SeatLimit: 5, Status: "active", MemberCount: 1},
		},
		members: map[string][]Member{
			"org-a": {
				{ID: "a1", Email: "owner@acme.test", Name: "Owner", OrgRole: "owner", CreatedAt: time.Unix(1, 0).UTC()},
				{ID: "a2", Email: "dev@acme.test", Name: "Dev", OrgRole: "member", CreatedAt: time.Unix(2, 0).UTC()},
			},
			"org-b": {
				{ID: "b1", Email: "spy@beta.test", Name: "Spy", OrgRole: "admin", CreatedAt: time.Unix(3, 0).UTC()},
			},
		},
	}
	rtr := orgAdminRouter(store)

	req := httptest.NewRequest(http.MethodGet, "/members", nil)
	req.Header.Set("Authorization", "Bearer t")
	w := httptest.NewRecorder()
	rtr.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var res ListMembersResponse
	if err := json.NewDecoder(w.Body).Decode(&res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(res.Members) != 2 {
		t.Fatalf("len(members) = %d, want 2", len(res.Members))
	}
	for _, m := range res.Members {
		if m.ID == "b1" || m.Email == "spy@beta.test" {
			t.Fatalf("org B member leaked into list: %+v", m)
		}
	}
	if store.lastID != "org-a" {
		t.Fatalf("store queried %q, want org-a", store.lastID)
	}
}
