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

	getMemberCalled   bool
	setRoleCalled     bool
	deleteCalled      bool
	lastSetRole       string
	lastMutatedUserID string

	// stats / activity: keyed by org id so cross-org exclusion is testable
	statsByOrg    map[string]OrgStats
	activityByOrg map[string][]ActivityItem
	statsOrgID    string
	activityOrgID string
	statsFrom     time.Time
	statsTo       time.Time
	activityLimit int
	activityBefore *time.Time
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

func (f *fakeOrgStore) GetMember(_ context.Context, userID string) (Member, string, error) {
	f.getMemberCalled = true
	f.lastID = userID
	for orgID, list := range f.members {
		for _, m := range list {
			if m.ID == userID {
				return m, orgID, nil
			}
		}
	}
	return Member{}, "", ErrNoRows
}

func (f *fakeOrgStore) SetMemberRole(_ context.Context, userID, orgRole string) (Member, error) {
	f.setRoleCalled = true
	f.lastMutatedUserID = userID
	f.lastSetRole = orgRole
	for orgID, list := range f.members {
		for i, m := range list {
			if m.ID == userID {
				m.OrgRole = orgRole
				f.members[orgID][i] = m
				return m, nil
			}
		}
	}
	return Member{}, ErrNoRows
}

func (f *fakeOrgStore) DeleteMember(_ context.Context, userID string) error {
	f.deleteCalled = true
	f.lastMutatedUserID = userID
	for orgID, list := range f.members {
		for i, m := range list {
			if m.ID == userID {
				f.members[orgID] = append(list[:i], list[i+1:]...)
				if s, ok := f.orgs[orgID]; ok {
					s.MemberCount--
					f.orgs[orgID] = s
				}
				return nil
			}
		}
	}
	return ErrNoRows
}

func (f *fakeOrgStore) Stats(_ context.Context, orgID string, from, to time.Time) (OrgStats, error) {
	f.statsOrgID = orgID
	f.statsFrom = from
	f.statsTo = to
	if f.statsByOrg == nil {
		return OrgStats{}, nil
	}
	return f.statsByOrg[orgID], nil
}

func (f *fakeOrgStore) Activity(_ context.Context, orgID string, limit int, before *time.Time) ([]ActivityItem, error) {
	f.activityOrgID = orgID
	f.activityLimit = limit
	f.activityBefore = before
	if f.activityByOrg == nil {
		return []ActivityItem{}, nil
	}
	items := f.activityByOrg[orgID]
	if items == nil {
		return []ActivityItem{}, nil
	}
	if limit > 0 && len(items) > limit {
		return items[:limit], nil
	}
	return items, nil
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
	h := NewHandler(store, nil, bcrypt.MinCost)
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
	h := NewHandler(store, nil, bcrypt.MinCost)
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
	h := NewHandler(store, nil, bcrypt.MinCost)
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
	h := NewHandler(store, nil, bcrypt.MinCost)
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

func seededMemberStore() *fakeOrgStore {
	return &fakeOrgStore{
		orgs: map[string]OrgSummary{
			"org-a": {ID: "org-a", Name: "Acme", Type: "company", SeatLimit: 5, Status: "active", MemberCount: 3},
			"org-b": {ID: "org-b", Name: "Beta", Type: "company", SeatLimit: 5, Status: "active", MemberCount: 1},
		},
		members: map[string][]Member{
			"org-a": {
				{ID: "owner-a", Email: "owner@acme.test", Name: "Owner", OrgRole: "owner", CreatedAt: time.Unix(1, 0).UTC()},
				{ID: "admin-a", Email: "admin@acme.test", Name: "Admin", OrgRole: "admin", CreatedAt: time.Unix(2, 0).UTC()},
				{ID: "member-a", Email: "dev@acme.test", Name: "Dev", OrgRole: "member", CreatedAt: time.Unix(3, 0).UTC()},
			},
			"org-b": {
				{ID: "admin-b", Email: "spy@beta.test", Name: "Spy", OrgRole: "admin", CreatedAt: time.Unix(4, 0).UTC()},
			},
		},
	}
}

func TestPatchMemberCrossOrg404(t *testing.T) {
	store := seededMemberStore()
	rtr := orgAdminRouter(store)

	body := bytes.NewBufferString(`{"org_role":"member"}`)
	req := httptest.NewRequest(http.MethodPatch, "/members/admin-b", body)
	req.Header.Set("Authorization", "Bearer t")
	w := httptest.NewRecorder()
	rtr.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", w.Code, w.Body.String())
	}
	if !store.getMemberCalled {
		t.Fatal("handler must re-read target row before mutate")
	}
	if store.setRoleCalled {
		t.Fatal("SetMemberRole must not run for cross-org target")
	}
}

func TestDeleteMemberCrossOrg404(t *testing.T) {
	store := seededMemberStore()
	rtr := orgAdminRouter(store)

	req := httptest.NewRequest(http.MethodDelete, "/members/admin-b", nil)
	req.Header.Set("Authorization", "Bearer t")
	w := httptest.NewRecorder()
	rtr.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", w.Code, w.Body.String())
	}
	if !store.getMemberCalled {
		t.Fatal("handler must re-read target row before mutate")
	}
	if store.deleteCalled {
		t.Fatal("DeleteMember must not run for cross-org target")
	}
}

func TestCannotChangeOrDeleteOwner(t *testing.T) {
	store := seededMemberStore()
	rtr := orgAdminRouter(store)

	body := bytes.NewBufferString(`{"org_role":"member"}`)
	req := httptest.NewRequest(http.MethodPatch, "/members/owner-a", body)
	req.Header.Set("Authorization", "Bearer t")
	w := httptest.NewRecorder()
	rtr.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("PATCH owner status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
	if store.setRoleCalled {
		t.Fatal("SetMemberRole must not run for owner")
	}

	req = httptest.NewRequest(http.MethodDelete, "/members/owner-a", nil)
	req.Header.Set("Authorization", "Bearer t")
	w = httptest.NewRecorder()
	rtr.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("DELETE owner status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
	if store.deleteCalled {
		t.Fatal("DeleteMember must not run for owner")
	}
	if !store.getMemberCalled {
		t.Fatal("handler must re-read target row before mutate")
	}
}

func TestPatchAdminToMember(t *testing.T) {
	store := seededMemberStore()
	rtr := orgAdminRouter(store)

	body := bytes.NewBufferString(`{"org_role":"member"}`)
	req := httptest.NewRequest(http.MethodPatch, "/members/admin-a", body)
	req.Header.Set("Authorization", "Bearer t")
	w := httptest.NewRecorder()
	rtr.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var res Member
	if err := json.NewDecoder(w.Body).Decode(&res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res.ID != "admin-a" || res.OrgRole != "member" {
		t.Fatalf("member = %+v, want admin-a / member", res)
	}
	if !store.getMemberCalled {
		t.Fatal("handler must re-read target row before mutate")
	}
	if !store.setRoleCalled || store.lastSetRole != "member" || store.lastMutatedUserID != "admin-a" {
		t.Fatalf("SetMemberRole not called correctly: called=%v role=%q id=%q",
			store.setRoleCalled, store.lastSetRole, store.lastMutatedUserID)
	}
}

func TestOrgStatsRejectsUnknownWindow(t *testing.T) {
	store := &fakeOrgStore{}
	rtr := orgAdminRouter(store)

	req := httptest.NewRequest(http.MethodGet, "/stats?window=7d", nil)
	req.Header.Set("Authorization", "Bearer t")
	w := httptest.NewRecorder()
	rtr.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
	if store.statsOrgID != "" {
		t.Fatal("store must not be queried for an invalid window")
	}
}

func TestOrgStatsExcludesOtherOrg(t *testing.T) {
	// Seed both orgs with distinct report counts. The handler must ask only for
	// claims.OrgID (org-a); the response must not carry org-b's numbers.
	store := &fakeOrgStore{
		statsByOrg: map[string]OrgStats{
			"org-a": {
				Boxes: OrgStatsBoxes{
					Members:        MemberSeatBox{Count: 3, SeatLimit: 5},
					ReportsLast24h: StatBox{Value: 2},
					ReportsWindow:  StatBox{Value: 10},
					SchemaValidity: SchemaBox{Rate: 0.8},
				},
				AssessmentsPerDay: []DayPoint{{T: 1, V: 10}},
				MemberActivity:    []MemberAct{{UserID: "a1", Name: "Ada", Count: 10}},
			},
			"org-b": {
				Boxes: OrgStatsBoxes{
					Members:        MemberSeatBox{Count: 99, SeatLimit: 100},
					ReportsLast24h: StatBox{Value: 50},
					ReportsWindow:  StatBox{Value: 999},
					SchemaValidity: SchemaBox{Rate: 0.1},
				},
				AssessmentsPerDay: []DayPoint{{T: 1, V: 999}},
				MemberActivity:    []MemberAct{{UserID: "b1", Name: "Spy", Count: 999}},
			},
		},
	}
	rtr := orgAdminRouter(store)

	req := httptest.NewRequest(http.MethodGet, "/stats?window=30d", nil)
	req.Header.Set("Authorization", "Bearer t")
	w := httptest.NewRecorder()
	rtr.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if store.statsOrgID != "org-a" {
		t.Fatalf("stats org = %q, want org-a", store.statsOrgID)
	}
	var res OrgStats
	if err := json.NewDecoder(w.Body).Decode(&res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res.Window != "30d" {
		t.Fatalf("window = %q, want 30d", res.Window)
	}
	if res.Boxes.Members.Count != 3 || res.Boxes.ReportsWindow.Value != 10 {
		t.Fatalf("boxes = %+v, want org-a figures", res.Boxes)
	}
	if res.Boxes.ReportsWindow.Value == 999 || res.Boxes.Members.Count == 99 {
		t.Fatal("org-b stats leaked into org-a response")
	}
	for _, m := range res.MemberActivity {
		if m.UserID == "b1" || m.Name == "Spy" {
			t.Fatalf("org-b member activity leaked: %+v", m)
		}
	}
	if res.AssessmentsPerDay == nil || res.SchemaValidPerDay == nil || res.RunsByTarget == nil {
		t.Fatal("nil slices must normalize to empty JSON arrays")
	}
}

func TestOrgActivityExcludesOtherOrg(t *testing.T) {
	store := &fakeOrgStore{
		activityByOrg: map[string][]ActivityItem{
			"org-a": {
				{ID: "a-1", Kind: ActivityAnalysisCompleted, At: time.Unix(10, 0).UTC(), ActorName: "Ada", Meta: map[string]any{"schema_valid": true}},
				{ID: "a-2", Kind: ActivityMemberJoined, At: time.Unix(5, 0).UTC(), ActorName: "Ada"},
			},
			"org-b": {
				{ID: "b-1", Kind: ActivityAnalysisCompleted, At: time.Unix(20, 0).UTC(), ActorName: "Spy", Meta: map[string]any{"schema_valid": true}},
			},
		},
	}
	rtr := orgAdminRouter(store)

	req := httptest.NewRequest(http.MethodGet, "/activity?limit=10", nil)
	req.Header.Set("Authorization", "Bearer t")
	w := httptest.NewRecorder()
	rtr.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if store.activityOrgID != "org-a" {
		t.Fatalf("activity org = %q, want org-a", store.activityOrgID)
	}
	if store.activityLimit != 10 {
		t.Fatalf("limit = %d, want 10", store.activityLimit)
	}
	var res ActivityResponse
	if err := json.NewDecoder(w.Body).Decode(&res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(res.Items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(res.Items))
	}
	for _, item := range res.Items {
		if item.ID == "b-1" || item.ActorName == "Spy" {
			t.Fatalf("org-b activity leaked: %+v", item)
		}
	}
}

func TestOrgActivityMetadataOnlyNoCaseText(t *testing.T) {
	// Redaction contract: the JSON body must not expose case fields even when
	// a buggy store tried to stuff them into Meta. The handler still serializes
	// Meta, so the store is the gate — this test locks the wire shape to id /
	// kind / at / actor_name / meta flags, and asserts forbidden keys absent.
	store := &fakeOrgStore{
		activityByOrg: map[string][]ActivityItem{
			"org-a": {{
				ID:        "assess-1",
				Kind:      ActivityAnalysisSchemaInvalid,
				At:        time.Unix(10, 0).UTC(),
				ActorName: "Ada",
				Meta:      map[string]any{"schema_valid": false},
			}},
		},
	}
	rtr := orgAdminRouter(store)

	req := httptest.NewRequest(http.MethodGet, "/activity", nil)
	req.Header.Set("Authorization", "Bearer t")
	w := httptest.NewRecorder()
	rtr.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, forbidden := range []string{
		"subject", "subject_title", "findings", "prompt",
		"raw_response", "transcript", "case_text", "overall_score",
	} {
		if containsJSONKey(body, forbidden) {
			t.Fatalf("activity body must not contain %q; body=%s", forbidden, body)
		}
	}
	var res ActivityResponse
	if err := json.NewDecoder(bytes.NewBufferString(body)).Decode(&res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(res.Items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(res.Items))
	}
	if res.Items[0].Kind != ActivityAnalysisSchemaInvalid {
		t.Fatalf("kind = %q, want %s", res.Items[0].Kind, ActivityAnalysisSchemaInvalid)
	}
	if v, ok := res.Items[0].Meta["schema_valid"].(bool); !ok || v {
		t.Fatalf("meta.schema_valid = %v, want false", res.Items[0].Meta["schema_valid"])
	}
}

func TestOrgActivityRejectsBadBefore(t *testing.T) {
	store := &fakeOrgStore{}
	rtr := orgAdminRouter(store)

	req := httptest.NewRequest(http.MethodGet, "/activity?before=not-a-time", nil)
	req.Header.Set("Authorization", "Bearer t")
	w := httptest.NewRecorder()
	rtr.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// containsJSONKey is a blunt check for a JSON object key in the response body.
// Good enough to catch accidental inclusion of case-content field names.
func containsJSONKey(body, key string) bool {
	needle := `"` + key + `"`
	return bytes.Contains([]byte(body), []byte(needle))
}
