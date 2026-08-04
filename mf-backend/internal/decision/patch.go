package decision

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/emrah/mf-backend/internal/common"
)

// patchOp describes one PATCH body. Title and assessment_id are independent:
// either may be omitted, but not both.
type patchOp struct {
	Title         *string // set title when non-nil
	SetAssessment bool    // true if assessment_id key was present
	AssessmentID  *string // nil means clear when SetAssessment
}

// AssessmentOwner answers whether a report id belongs to the caller. Injected
// from analysis so decision does not import analysis types or duplicate the
// ownership query.
type AssessmentOwner interface {
	OwnsAssessment(ctx context.Context, userID, id string) (bool, error)
}

// parsePatchBody interprets a partial PATCH payload. Keys are optional but at
// least one of title or assessment_id must be present — an empty object would
// be a no-op the client cannot distinguish from success.
func parsePatchBody(data []byte) (patchOp, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return patchOp{}, errors.New("invalid JSON body")
	}

	var op patchOp
	if titleRaw, ok := raw["title"]; ok {
		var title string
		if err := json.Unmarshal(titleRaw, &title); err != nil {
			return patchOp{}, errors.New("title must be a string")
		}
		title = strings.TrimSpace(title)
		if title == "" {
			return patchOp{}, errors.New("title is required")
		}
		op.Title = &title
	}
	if assessRaw, ok := raw["assessment_id"]; ok {
		op.SetAssessment = true
		if string(assessRaw) == "null" {
			op.AssessmentID = nil
		} else {
			var id string
			if err := json.Unmarshal(assessRaw, &id); err != nil {
				return patchOp{}, errors.New("assessment_id must be a string or null")
			}
			id = strings.TrimSpace(id)
			if id == "" {
				return patchOp{}, errors.New("assessment_id must not be empty")
			}
			op.AssessmentID = &id
		}
	}
	if op.Title == nil && !op.SetAssessment {
		return patchOp{}, errors.New("title or assessment_id is required")
	}
	return op, nil
}

// Patch updates a thread's title and/or linked report. PATCH /decision/conversations/{id}
func (h *Handler) Patch(w http.ResponseWriter, r *http.Request) {
	claims, _ := common.ClaimsFromContext(r.Context())

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		common.Error(w, common.ErrBadRequest("request body too large"))
		return
	}

	op, err := parsePatchBody(data)
	if err != nil {
		common.Error(w, common.ErrBadRequest(err.Error()))
		return
	}

	ctx := r.Context()
	id := chi.URLParam(r, "id")

	// Linking checks ownership before touching the conversation row. A foreign
	// report gets 404, not 403, so the response does not confirm the id exists.
	if op.SetAssessment && op.AssessmentID != nil {
		owns, err := h.assessments.OwnsAssessment(ctx, claims.UserID, *op.AssessmentID)
		if err != nil {
			slog.Error("decision patch: assessment ownership check failed", "error", err)
			common.Error(w, common.ErrInternal("could not verify the report"))
			return
		}
		if !owns {
			common.Error(w, common.ErrNotFound("no such conversation"))
			return
		}
	}

	if op.Title != nil {
		if err := h.store.Rename(ctx, claims.UserID, id, *op.Title); errors.Is(err, ErrNoRows) {
			common.Error(w, common.ErrNotFound("no such conversation"))
			return
		} else if err != nil {
			slog.Error("decision patch rename failed", "error", err)
			common.Error(w, common.ErrInternal("could not rename the conversation"))
			return
		}
	}
	if op.SetAssessment {
		if err := h.store.SetAssessmentID(ctx, claims.UserID, id, op.AssessmentID); errors.Is(err, ErrNoRows) {
			common.Error(w, common.ErrNotFound("no such conversation"))
			return
		} else if err != nil {
			slog.Error("decision patch assessment link failed", "error", err)
			common.Error(w, common.ErrInternal("could not update the conversation"))
			return
		}
	}

	w.WriteHeader(http.StatusNoContent)
}
