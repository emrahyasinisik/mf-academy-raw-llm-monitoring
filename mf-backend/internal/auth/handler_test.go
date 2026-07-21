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
	"golang.org/x/crypto/bcrypt"
)

// fakeStore is an in-memory UserStore. The handlers depend on the interface
// rather than *Store, so these tests need no database.
type fakeStore struct {
	user     User
	hash     string
	notFound bool

	lookup      SessionLookup
	lookupErr   error
	revokedAll  int
	revokeAllFn func(userID string) (int64, error)
}

func (f *fakeStore) CreateUser(context.Context, string, string, string) (User, error) {
	return f.user, nil
}

func (f *fakeStore) GetUserByEmailWithHash(_ context.Context, _ string) (User, string, error) {
	if f.notFound {
		return User{}, "", ErrNoRows
	}
	return f.user, f.hash, nil
}

func (f *fakeStore) GetUserByID(context.Context, string) (User, error) { return f.user, nil }
func (f *fakeStore) GetPasswordHash(context.Context, string) (string, error) {
	return f.hash, nil
}
func (f *fakeStore) UpdateName(context.Context, string, string) (User, error) { return f.user, nil }
func (f *fakeStore) UpdatePassword(context.Context, string, string) error     { return nil }

func (f *fakeStore) CreateSession(context.Context, string, string, string, string, time.Time) (string, error) {
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

func postJSON(h http.HandlerFunc, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/auth/login", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
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
