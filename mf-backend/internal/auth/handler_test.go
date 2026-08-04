package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/emrah/mf-backend/internal/common"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

// fakeStore is an in-memory UserStore. The handlers depend on the interface
// rather than *Store, so these tests need no database.
type fakeStore struct {
	user               User
	hash               string
	notFound           bool
	mustChangePassword map[string]bool

	lookup      SessionLookup
	lookupErr   error
	revokedAll  int
	revokeAllFn func(userID string) (int64, error)

	created         int // number of CreateUser calls; a refused registration must leave this at 0
	sessionsCreated int
	acceptedVersion string // version passed to the most recent AcceptTerms call
}

func (f *fakeStore) CreateUser(context.Context, string, string, string, string) (User, error) {
	f.created++
	return f.user, nil
}

func (f *fakeStore) AcceptTerms(_ context.Context, _ string, version string) error {
	f.acceptedVersion = version
	return nil
}

func (f *fakeStore) GetUserByEmailWithHash(_ context.Context, _ string) (User, string, error) {
	if f.notFound {
		return User{}, "", ErrNoRows
	}
	return f.userWithPasswordFlag(f.user), f.hash, nil
}

func (f *fakeStore) userWithPasswordFlag(u User) User {
	if f.mustChangePassword != nil {
		u.MustChangePassword = f.mustChangePassword[u.ID]
	}
	return u
}

func (f *fakeStore) GetUserByID(_ context.Context, id string) (User, error) {
	u := f.user
	u.ID = id
	return f.userWithPasswordFlag(u), nil
}
func (f *fakeStore) GetPasswordHash(context.Context, string) (string, error) {
	return f.hash, nil
}
func (f *fakeStore) UpdateName(context.Context, string, string) (User, error) { return f.user, nil }
func (f *fakeStore) UpdatePassword(_ context.Context, id, _ string) error {
	if f.mustChangePassword != nil {
		f.mustChangePassword[id] = false
	}
	return nil
}

func (f *fakeStore) CreateSession(context.Context, string, string, string, string, time.Time) (string, error) {
	f.sessionsCreated++
	return "session-1", nil
}
func (f *fakeStore) FindValidSessionByHash(context.Context, string) (string, string, error) {
	return f.lookup.SessionID, f.lookup.UserID, f.lookupErr
}
func (f *fakeStore) FindSessionByHashAnyState(context.Context, string) (SessionLookup, error) {
	return f.lookup, f.lookupErr
}
func (f *fakeStore) RevokeSession(context.Context, string) error                { return nil }
func (f *fakeStore) RevokeSessionForUser(context.Context, string, string) error { return nil }
func (f *fakeStore) RevokeAllSessionsForUser(_ context.Context, userID string) (int64, error) {
	if f.revokeAllFn != nil {
		return f.revokeAllFn(userID)
	}
	f.revokedAll++
	return 3, nil
}
func (f *fakeStore) ListSessions(context.Context, string) ([]Session, error) { return nil, nil }

// testHashCost keeps the suite fast. It sits at the enforced floor, so the
// timing test still exercises real bcrypt work on both paths — the property
// under test is that the two are equal, not how long either takes.
const testHashCost = MinHashCost

func newTestHandler(store UserStore) *Handler {
	return NewHandler(store, NewTokenService(strings.Repeat("s", 48), time.Minute, time.Hour), testHashCost)
}

// newTestHandlerWithStore is newTestHandler plus the concrete fake, for tests
// that need to inspect what the handler did to the store afterward (call
// counts, recorded values) rather than only the HTTP response.
func newTestHandlerWithStore() (*Handler, *fakeStore) {
	st := &fakeStore{}
	return newTestHandler(st), st
}

func postJSON(h http.HandlerFunc, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/auth/login", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h(w, r)
	return w
}

// authedPost issues a body-less POST carrying claims for userID, for
// endpoints like AcceptTerms that read only the caller's identity.
func authedPost(h http.HandlerFunc, userID string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/auth/accept-terms", nil)
	r = r.WithContext(common.ContextWithClaims(r.Context(), common.AuthClaims{UserID: userID}))
	w := httptest.NewRecorder()
	h(w, r)
	return w
}

// Login must spend the same bcrypt work on a missing account as on a real one.
// Before the decoy hash, an unknown address returned in ~0.5ms against ~50ms
// for a known one — a single-request oracle for whether an account exists.
func TestLoginSpendsEqualWorkOnUnknownAccount(t *testing.T) {
	realHash, err := bcrypt.GenerateFromPassword([]byte("correct-password"), testHashCost)
	if err != nil {
		t.Fatalf("seeding hash: %v", err)
	}

	existing := newTestHandler(&fakeStore{
		user: User{ID: "u1", Email: "victim@corp.io"},
		hash: string(realHash),
	})
	missing := newTestHandler(&fakeStore{notFound: true})

	const body = `{"email":"victim@corp.io","password":"wrong-guess"}`

	start := time.Now()
	wExisting := postJSON(existing.Login, body)
	existingDur := time.Since(start)

	start = time.Now()
	wMissing := postJSON(missing.Login, body)
	missingDur := time.Since(start)

	if wExisting.Code != http.StatusUnauthorized || wMissing.Code != http.StatusUnauthorized {
		t.Fatalf("status codes = %d/%d, want 401 for both", wExisting.Code, wMissing.Code)
	}
	if wExisting.Body.String() != wMissing.Body.String() {
		t.Errorf("response bodies differ:\n existing: %s\n missing:  %s",
			wExisting.Body.String(), wMissing.Body.String())
	}

	// Allow generous slack for scheduling noise; the bug being guarded against
	// was two orders of magnitude, not a few percent.
	ratio := float64(existingDur) / float64(missingDur)
	if ratio > 3 || ratio < 0.33 {
		t.Errorf("timing ratio %.1fx (existing %v, missing %v); the two paths must be indistinguishable",
			ratio, existingDur, missingDur)
	}
}

func TestLoginRejectsSuspendedOrgMember(t *testing.T) {
	realHash, err := bcrypt.GenerateFromPassword([]byte("correct-password"), testHashCost)
	if err != nil {
		t.Fatalf("seeding hash: %v", err)
	}
	store := &fakeStore{
		user: User{ID: "u1", Email: "member@corp.io", OrgStatus: "suspended"},
		hash: string(realHash),
	}
	h := newTestHandler(store)

	wSuspended := postJSON(h.Login, `{"email":"member@corp.io","password":"correct-password"}`)
	wMissing := postJSON(newTestHandler(&fakeStore{notFound: true}).Login,
		`{"email":"member@corp.io","password":"correct-password"}`)

	if wSuspended.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", wSuspended.Code, wSuspended.Body.String())
	}
	if wSuspended.Body.String() != wMissing.Body.String() {
		t.Fatalf("suspended response differs from invalid login:\n suspended: %s\n invalid:    %s",
			wSuspended.Body.String(), wMissing.Body.String())
	}
	if store.sessionsCreated != 0 {
		t.Fatalf("CreateSession called %d times, want 0", store.sessionsCreated)
	}
}

// Presenting an already-revoked refresh token is evidence the token was
// captured: rotation means the legitimate holder swapped it. Both copies must
// be retired rather than the request merely failing.
func TestRefreshRevokesEverythingOnTokenReuse(t *testing.T) {
	store := &fakeStore{
		user:   User{ID: "u1"},
		lookup: SessionLookup{SessionID: "s1", UserID: "u1", Revoked: true},
	}
	h := newTestHandler(store)

	r := httptest.NewRequest("POST", "/auth/refresh", strings.NewReader(`{"refresh_token":"stolen"}`))
	w := httptest.NewRecorder()
	h.Refresh(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
	if store.revokedAll != 1 {
		t.Errorf("RevokeAllSessionsForUser called %d times, want 1", store.revokedAll)
	}
}

func TestRefreshRejectsExpiredWithoutMassRevocation(t *testing.T) {
	store := &fakeStore{
		user:   User{ID: "u1"},
		lookup: SessionLookup{SessionID: "s1", UserID: "u1", Expired: true},
	}
	h := newTestHandler(store)

	r := httptest.NewRequest("POST", "/auth/refresh", strings.NewReader(`{"refresh_token":"old"}`))
	w := httptest.NewRecorder()
	h.Refresh(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
	// Expiry is routine, not evidence of theft — it must not sign the user out
	// of every other device.
	if store.revokedAll != 0 {
		t.Errorf("RevokeAllSessionsForUser called %d times on plain expiry, want 0", store.revokedAll)
	}
}

func TestRefreshRejectsSuspendedOrgMember(t *testing.T) {
	store := &fakeStore{
		user:   User{ID: "u1", Email: "member@corp.io", OrgStatus: "suspended"},
		lookup: SessionLookup{SessionID: "s1", UserID: "u1"},
	}
	h := newTestHandler(store)

	r := httptest.NewRequest("POST", "/auth/refresh", strings.NewReader(`{"refresh_token":"still-live"}`))
	w := httptest.NewRecorder()
	h.Refresh(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", w.Code, w.Body.String())
	}
	if store.sessionsCreated != 0 {
		t.Fatalf("CreateSession called %d times, want 0", store.sessionsCreated)
	}
	var errBody common.ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("error body did not decode: %v", err)
	}
	if errBody.Error != "unauthorized" {
		t.Fatalf("error code = %q, want unauthorized", errBody.Error)
	}
}

// A password change exists to eject someone. It is worthless if the intruder's
// refresh token keeps working afterwards.
func TestChangePasswordRevokesSessionsAndReissues(t *testing.T) {
	current, err := bcrypt.GenerateFromPassword([]byte("old-password"), testHashCost)
	if err != nil {
		t.Fatalf("seeding hash: %v", err)
	}
	store := &fakeStore{user: User{ID: "u1", Email: "a@b.io"}, hash: string(current)}
	h := newTestHandler(store)

	r := httptest.NewRequest("POST", "/auth/change-password",
		strings.NewReader(`{"current_password":"old-password","new_password":"a-new-password"}`))
	r = r.WithContext(common.ContextWithClaims(r.Context(), common.AuthClaims{UserID: "u1"}))
	w := httptest.NewRecorder()
	h.ChangePassword(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if store.revokedAll != 1 {
		t.Errorf("RevokeAllSessionsForUser called %d times, want 1", store.revokedAll)
	}

	// The caller must not be locked out by their own password change.
	var out TokenPair
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("response did not decode as a token pair: %v", err)
	}
	if out.AccessToken == "" || out.RefreshToken == "" {
		t.Error("response carries no fresh token pair")
	}
}

func TestPasswordResetClaimBlocksProductButNotAuth(t *testing.T) {
	current, err := bcrypt.GenerateFromPassword([]byte("old-password"), testHashCost)
	if err != nil {
		t.Fatalf("seeding hash: %v", err)
	}
	store := &fakeStore{
		user:               User{ID: "u1", Email: "a@b.io"},
		hash:               string(current),
		mustChangePassword: map[string]bool{"u1": true},
	}
	h := newTestHandler(store)

	access, _, err := h.tokens.GenerateAccess(store.userWithPasswordFlag(store.user))
	if err != nil {
		t.Fatalf("GenerateAccess: %v", err)
	}
	claims, err := h.tokens.Verify(access)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !claims.PasswordReset {
		t.Fatal("verified claims did not preserve pwd_reset")
	}

	nextCalled := false
	product := common.RequirePasswordFresh(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		nextCalled = true
	}))
	r := httptest.NewRequest(http.MethodGet, "/analysis", nil)
	r = r.WithContext(common.ContextWithClaims(r.Context(), claims))
	w := httptest.NewRecorder()
	product.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("product status = %d, want 403; body=%s", w.Code, w.Body.String())
	}
	if nextCalled {
		t.Fatal("product handler ran despite password reset requirement")
	}
	var errBody common.ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("error body did not decode: %v", err)
	}
	if errBody.Error != "password_change_required" {
		t.Fatalf("error code = %q, want password_change_required", errBody.Error)
	}

	r = httptest.NewRequest("POST", "/auth/change-password",
		strings.NewReader(`{"current_password":"old-password","new_password":"a-new-password"}`))
	r = r.WithContext(common.ContextWithClaims(r.Context(), common.AuthClaims{UserID: "u1", PasswordReset: true}))
	w = httptest.NewRecorder()
	h.ChangePassword(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("change password status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var out TokenPair
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("response did not decode as a token pair: %v", err)
	}
	claims, err = h.tokens.Verify(out.AccessToken)
	if err != nil {
		t.Fatalf("Verify changed-password access token: %v", err)
	}
	if claims.PasswordReset {
		t.Fatal("changed-password access token still has pwd_reset")
	}
}

func TestRefreshPreservesPasswordResetFlag(t *testing.T) {
	tokens := NewTokenService(strings.Repeat("s", 48), time.Minute, time.Hour)
	refresh, hash, _, err := tokens.GenerateRefresh()
	if err != nil {
		t.Fatalf("GenerateRefresh: %v", err)
	}
	store := &fakeStore{
		user:               User{ID: "u1", Email: "a@b.io"},
		lookup:             SessionLookup{SessionID: "s1", UserID: "u1"},
		mustChangePassword: map[string]bool{"u1": true},
	}
	h := NewHandler(store, tokens, testHashCost)
	store.lookup = SessionLookup{SessionID: "s1", UserID: "u1"}
	if HashToken(refresh) != hash {
		t.Fatal("refresh hash seed mismatch")
	}

	r := httptest.NewRequest("POST", "/auth/refresh", strings.NewReader(`{"refresh_token":"`+refresh+`"}`))
	w := httptest.NewRecorder()
	h.Refresh(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var out TokenPair
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("response did not decode as a token pair: %v", err)
	}
	claims, err := h.tokens.Verify(out.AccessToken)
	if err != nil {
		t.Fatalf("Verify refreshed access token: %v", err)
	}
	if !claims.PasswordReset {
		t.Fatal("refreshed access token lost pwd_reset")
	}
}

// Registration must refuse anyone who accepted nothing — an absent field and
// an explicit false are refused identically, since the wire has no way to
// distinguish "never asked" from "asked and declined".
func TestRegisterRefusesWithoutAcceptance(t *testing.T) {
	for _, body := range []string{
		`{"email":"a@b.co","password":"parola12345","name":"A"}`,
		`{"email":"a@b.co","password":"parola12345","name":"A","accepted_terms":false}`,
	} {
		h := newTestHandler(&fakeStore{})
		w := postJSON(h.Register, body)
		if w.Code != http.StatusBadRequest {
			t.Errorf("body %s -> status %d, want 400", body, w.Code)
		}
	}
}

// Kabul edilmemis bir kayit hic olusmamali: 400 donup kullaniciyi yine de
// yaratmak, kabulu olmayan bir hesap birakir ve kapi onu yakalamaz.
func TestRegisterDoesNotCreateUserWhenRefused(t *testing.T) {
	h, st := newTestHandlerWithStore()
	postJSON(h.Register, `{"email":"a@b.co","password":"parola12345","name":"A"}`)
	if st.created != 0 {
		t.Errorf("CreateUser called %d times, want 0", st.created)
	}
}

func TestCreateUserCreatesIndividualOrgInTransaction(t *testing.T) {
	tx := &recordingTx{
		rows: []pgx.Row{
			scanRow{values: []any{"org-1"}},
			scanRow{values: []any{
				"user-1", "new@user.io", "New User", "user", false,
				time.Unix(1, 0), time.Unix(2, 0), ptr(time.Unix(3, 0)),
			}},
		},
	}

	user, err := createUserWithIndividualOrg(context.Background(), func(context.Context) (authTx, error) {
		tx.began = true
		return tx, nil
	}, "new@user.io", "hash", "New User", TermsVersion)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if !tx.began || !tx.committed || tx.rolledBack != 1 {
		t.Fatalf("transaction flags began=%v committed=%v rolledBack=%d, want began+committed+deferred rollback",
			tx.began, tx.committed, tx.rolledBack)
	}
	if len(tx.queries) != 2 {
		t.Fatalf("queries = %d, want 2", len(tx.queries))
	}
	if !strings.Contains(tx.queries[0], "INSERT INTO organizations") ||
		!strings.Contains(tx.queries[0], "VALUES ($1, 'individual', 1)") {
		t.Fatalf("organization query did not create an individual org:\n%s", tx.queries[0])
	}
	if tx.args[0][0] != "New User" {
		t.Fatalf("organization name arg = %#v, want display name", tx.args[0][0])
	}
	if !strings.Contains(tx.queries[1], "INSERT INTO users") ||
		!strings.Contains(tx.queries[1], "org_id") ||
		!strings.Contains(tx.queries[1], "must_change_password") {
		t.Fatalf("user query did not write org_id and password-reset flag:\n%s", tx.queries[1])
	}
	if tx.args[1][3] != "org-1" {
		t.Fatalf("user org_id arg = %#v, want org-1", tx.args[1][3])
	}
	if user.ID != "user-1" || user.Email != "new@user.io" {
		t.Fatalf("returned user = %#v", user)
	}
}

// AcceptTerms is the catch-up path for accounts that predate the terms. It
// must be safe to call twice: the caller may not know whether they already
// accepted, and a second call is success, not an error.
func TestAcceptTermsRecordsAndIsIdempotent(t *testing.T) {
	h, st := newTestHandlerWithStore()
	for i := 0; i < 2; i++ {
		w := authedPost(h.AcceptTerms, "user-1")
		if w.Code != http.StatusNoContent {
			t.Fatalf("call %d -> status %d, want 204", i+1, w.Code)
		}
	}
	if st.acceptedVersion != TermsVersion {
		t.Errorf("stored version %q, want %q", st.acceptedVersion, TermsVersion)
	}
}

type recordingTx struct {
	began      bool
	committed  bool
	rolledBack int
	rows       []pgx.Row
	queries    []string
	args       [][]any
}

func (t *recordingTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	t.queries = append(t.queries, sql)
	t.args = append(t.args, args)
	row := t.rows[0]
	t.rows = t.rows[1:]
	return row
}

func (t *recordingTx) Commit(context.Context) error {
	t.committed = true
	return nil
}

func (t *recordingTx) Rollback(context.Context) error {
	t.rolledBack++
	return nil
}

type scanRow struct {
	values []any
	err    error
}

func (r scanRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i := range dest {
		switch d := dest[i].(type) {
		case *string:
			*d = r.values[i].(string)
		case *bool:
			*d = r.values[i].(bool)
		case *time.Time:
			*d = r.values[i].(time.Time)
		case **time.Time:
			*d = r.values[i].(*time.Time)
		default:
			panic("unsupported scan destination")
		}
	}
	return nil
}

func ptr[T any](v T) *T { return &v }
