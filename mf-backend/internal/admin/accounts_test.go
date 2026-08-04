package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"
)

type fakeAccountStore struct {
	createErr error

	createdType  string
	createdName  string
	createdEmail string
	createdTaxID string
	createdSeats int
	storedHash   string

	suspendErr   error
	suspendCalls []string
	statusCalls  []struct {
		id     string
		status string
	}
}

func (f *fakeAccountStore) ListAccounts(context.Context, AccountListQuery) (AccountListResult, error) {
	return AccountListResult{}, nil
}

func (f *fakeAccountStore) CreateIndividual(_ context.Context, name, email, hash string) (AccountSummary, AccountMember, error) {
	f.createdType = accountTypeIndividual
	f.createdName = name
	f.createdEmail = email
	f.storedHash = hash
	if f.createErr != nil {
		return AccountSummary{}, AccountMember{}, f.createErr
	}
	now := time.Now()
	return AccountSummary{ID: "org-1", Name: name, Type: accountTypeIndividual, SeatLimit: 1, Status: accountStatusActive, MemberCount: 1, CreatedAt: now},
		AccountMember{ID: "user-1", Email: email, Name: name, OrgRole: "owner", CreatedAt: now},
		nil
}

func (f *fakeAccountStore) CreateCompany(_ context.Context, orgName, taxID string, seats int, ownerName, ownerEmail, hash string) (AccountSummary, AccountMember, error) {
	f.createdType = accountTypeCompany
	f.createdName = orgName
	f.createdEmail = ownerEmail
	f.createdTaxID = taxID
	f.createdSeats = seats
	f.storedHash = hash
	if f.createErr != nil {
		return AccountSummary{}, AccountMember{}, f.createErr
	}
	now := time.Now()
	return AccountSummary{ID: "org-1", Name: orgName, Type: accountTypeCompany, TaxID: taxID, SeatLimit: seats, Status: accountStatusActive, MemberCount: 1, CreatedAt: now},
		AccountMember{ID: "user-1", Email: ownerEmail, Name: ownerName, OrgRole: "owner", CreatedAt: now},
		nil
}

func (f *fakeAccountStore) GetAccount(context.Context, string) (AccountDetail, error) {
	return AccountDetail{}, nil
}

func (f *fakeAccountStore) SuspendAccount(_ context.Context, id string) error {
	f.suspendCalls = append(f.suspendCalls, id)
	return f.suspendErr
}

func (f *fakeAccountStore) SetAccountStatus(_ context.Context, id, status string) error {
	f.statusCalls = append(f.statusCalls, struct {
		id     string
		status string
	}{id: id, status: status})
	return nil
}

func accountsRouter(h *Handler) http.Handler {
	r := chi.NewRouter()
	h.localRoutes(r)
	return r
}

func TestCreateAccountReturnsTemporaryPasswordAndStoresOnlyHash(t *testing.T) {
	store := &fakeAccountStore{}
	h := &Handler{accounts: store, bcryptCost: bcrypt.MinCost}
	body := bytes.NewBufferString(`{"type":"individual","name":"Ada Lovelace","email":"ADA@example.com"}`)
	req := httptest.NewRequest(http.MethodPost, "/accounts/", body)
	w := httptest.NewRecorder()

	accountsRouter(h).ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d: %s", w.Code, w.Body.String())
	}
	var res CreateAccountResponse
	if err := json.NewDecoder(w.Body).Decode(&res); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if res.TemporaryPassword == "" {
		t.Fatal("temporary password must be returned once in the create response")
	}
	if store.storedHash == "" || store.storedHash == res.TemporaryPassword {
		t.Fatalf("store must receive a hash, got %q", store.storedHash)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(store.storedHash), []byte(res.TemporaryPassword)); err != nil {
		t.Fatalf("stored hash does not match temporary password: %v", err)
	}
	if store.createdEmail != "ada@example.com" {
		t.Fatalf("email should be normalized before storage, got %q", store.createdEmail)
	}
}

func TestCreateCompanyFailureDoesNotReturnTemporaryPassword(t *testing.T) {
	store := &fakeAccountStore{createErr: errors.New("fail after org insert")}
	h := &Handler{accounts: store, bcryptCost: bcrypt.MinCost}
	body := bytes.NewBufferString(`{"type":"company","name":"Acme AS","email":"owner@example.com","tax_id":"123","seat_limit":4}`)
	req := httptest.NewRequest(http.MethodPost, "/accounts/", body)
	w := httptest.NewRecorder()

	accountsRouter(h).ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d: %s", w.Code, w.Body.String())
	}
	if bytes.Contains(w.Body.Bytes(), []byte("temporary_password")) {
		t.Fatalf("failed creates must not leak the generated temporary password: %s", w.Body.String())
	}
	if store.createdType != accountTypeCompany || store.createdSeats != 4 || store.createdTaxID != "123" {
		t.Fatalf("company request was not passed through correctly: type=%q seats=%d tax=%q",
			store.createdType, store.createdSeats, store.createdTaxID)
	}
}

func TestSuspendAccountUsesAtomicStoreOperation(t *testing.T) {
	store := &fakeAccountStore{}
	h := &Handler{accounts: store, bcryptCost: bcrypt.MinCost}
	req := httptest.NewRequest(http.MethodPost, "/accounts/org-1/suspend", nil)
	w := httptest.NewRecorder()

	accountsRouter(h).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(store.suspendCalls) != 1 || store.suspendCalls[0] != "org-1" {
		t.Fatalf("want one atomic suspend call for org-1, got %v", store.suspendCalls)
	}
	if len(store.statusCalls) != 0 {
		t.Fatalf("handler must not split suspend into status/revoke calls, got %+v", store.statusCalls)
	}
}

func TestSuspendAccountReportsAtomicStoreFailure(t *testing.T) {
	store := &fakeAccountStore{suspendErr: errors.New("revoke failed")}
	h := &Handler{accounts: store, bcryptCost: bcrypt.MinCost}
	req := httptest.NewRequest(http.MethodPost, "/accounts/org-1/suspend", nil)
	w := httptest.NewRecorder()

	accountsRouter(h).ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d: %s", w.Code, w.Body.String())
	}
	if len(store.suspendCalls) != 1 || store.suspendCalls[0] != "org-1" {
		t.Fatalf("want one atomic suspend call for org-1, got %v", store.suspendCalls)
	}
}

func TestSuspendAccountRollsBackWhenSessionRevokeFails(t *testing.T) {
	tx := &recordingAccountTx{execErrAt: 2}

	err := suspendAccount(context.Background(), func(context.Context) (accountTx, error) {
		return tx, nil
	}, "org-1")

	if err == nil {
		t.Fatal("want session revoke error")
	}
	if tx.committed {
		t.Fatal("failed revoke must not commit the suspended status")
	}
	if tx.rolledBack != 1 {
		t.Fatalf("rollback count = %d, want 1", tx.rolledBack)
	}
	if len(tx.execs) != 2 {
		t.Fatalf("exec count = %d, want org update + session revoke", len(tx.execs))
	}
	if !strings.Contains(tx.execs[0], "UPDATE organizations") ||
		!strings.Contains(tx.execs[0], "status = 'suspended'") {
		t.Fatalf("first exec must suspend the org:\n%s", tx.execs[0])
	}
	if !strings.Contains(tx.execs[1], "UPDATE sessions") ||
		!strings.Contains(tx.execs[1], "FROM users") ||
		!strings.Contains(tx.execs[1], "u.org_id = $1") {
		t.Fatalf("second exec must revoke sessions by org in the same transaction:\n%s", tx.execs[1])
	}
}

type recordingAccountTx struct {
	execErrAt  int
	execs      []string
	committed  bool
	rolledBack int
}

func (t *recordingAccountTx) QueryRow(context.Context, string, ...any) pgx.Row {
	panic("QueryRow is not used by suspendAccount")
}

func (t *recordingAccountTx) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	t.execs = append(t.execs, sql)
	if t.execErrAt == len(t.execs) {
		return pgconn.CommandTag{}, errors.New("revoke failed")
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (t *recordingAccountTx) Commit(context.Context) error {
	t.committed = true
	return nil
}

func (t *recordingAccountTx) Rollback(context.Context) error {
	t.rolledBack++
	return nil
}
