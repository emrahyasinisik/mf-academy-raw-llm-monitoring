package decision

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/emrah/mf-backend/internal/common"
	"github.com/go-chi/chi/v5"
)

func TestParsePatchBodyTitleOnly(t *testing.T) {
	op, err := parsePatchBody([]byte(`{"title":"Acme"}`))
	if err != nil {
		t.Fatal(err)
	}
	if op.Title == nil || *op.Title != "Acme" {
		t.Fatalf("title=%v", op.Title)
	}
	if op.SetAssessment {
		t.Fatal("assessment must be omitted")
	}
}

func TestParsePatchBodyClearAssessment(t *testing.T) {
	op, err := parsePatchBody([]byte(`{"assessment_id":null}`))
	if err != nil {
		t.Fatal(err)
	}
	if !op.SetAssessment || op.AssessmentID != nil {
		t.Fatalf("want clear, got %+v", op)
	}
}

func TestParsePatchBodyEmptyRejected(t *testing.T) {
	_, err := parsePatchBody([]byte(`{}`))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParsePatchBodyBlankTitleRejected(t *testing.T) {
	_, err := parsePatchBody([]byte(`{"title":"  "}`))
	if err == nil {
		t.Fatal("expected error")
	}
}

// patchStore stubs only what Patch touches. The other conversationStore methods
// exist so the fake satisfies the handler field without a database.
type patchStore struct {
	renameErr           error
	setAssessmentErr    error
	gotRenameTitle      string
	gotAssessmentID     *string
	setAssessmentCalled bool
}

func (s *patchStore) Record(context.Context, string, string, string, Result) (string, error) {
	return "", nil
}
func (s *patchStore) List(context.Context, string, int, time.Time) (ListResult, error) {
	return ListResult{}, nil
}
func (s *patchStore) Get(context.Context, string, string) (Conversation, error) {
	return Conversation{}, nil
}
func (s *patchStore) Delete(context.Context, string, string) error { return nil }

func (s *patchStore) Rename(_ context.Context, _, _, title string) error {
	s.gotRenameTitle = title
	return s.renameErr
}

func (s *patchStore) SetAssessmentID(_ context.Context, _, _ string, assessmentID *string) error {
	s.setAssessmentCalled = true
	if assessmentID != nil {
		id := *assessmentID
		s.gotAssessmentID = &id
	}
	return s.setAssessmentErr
}

type fakeAssessmentOwner struct {
	owns bool
	err  error
}

func (f *fakeAssessmentOwner) OwnsAssessment(context.Context, string, string) (bool, error) {
	return f.owns, f.err
}

func patchRequest(t *testing.T, h *Handler, convID, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPatch, "/decision/conversations/"+convID, bytes.NewReader([]byte(body)))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", convID)
	r = r.WithContext(common.ContextWithClaims(
		context.WithValue(r.Context(), chi.RouteCtxKey, rctx),
		common.AuthClaims{UserID: "user-1"}))
	w := httptest.NewRecorder()
	h.Patch(w, r)
	return w
}

func TestPatchSetsAssessmentWhenOwned(t *testing.T) {
	st := &patchStore{}
	owner := &fakeAssessmentOwner{owns: true}
	h := &Handler{store: st, assessments: owner}

	w := patchRequest(t, h, "conv-1", `{"assessment_id":"rep-1"}`)
	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", w.Code)
	}
	if !st.setAssessmentCalled || st.gotAssessmentID == nil || *st.gotAssessmentID != "rep-1" {
		t.Errorf("SetAssessmentID = %v, want rep-1", st.gotAssessmentID)
	}
}

// 404 rather than 403: confirming the assessment id exists is information the
// caller is not entitled to when it belongs to someone else.
func TestPatchRejectsForeignAssessment(t *testing.T) {
	st := &patchStore{}
	owner := &fakeAssessmentOwner{owns: false}
	h := &Handler{store: st, assessments: owner}

	w := patchRequest(t, h, "conv-1", `{"assessment_id":"rep-other"}`)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
	if st.setAssessmentCalled {
		t.Fatal("SetAssessmentID must not run for a foreign report")
	}
}

func TestPatchClearsAssessment(t *testing.T) {
	st := &patchStore{}
	h := &Handler{store: st, assessments: &fakeAssessmentOwner{}}

	w := patchRequest(t, h, "conv-1", `{"assessment_id":null}`)
	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", w.Code)
	}
	if !st.setAssessmentCalled || st.gotAssessmentID != nil {
		t.Fatalf("want clear, got assessmentID=%v called=%v", st.gotAssessmentID, st.setAssessmentCalled)
	}
}
