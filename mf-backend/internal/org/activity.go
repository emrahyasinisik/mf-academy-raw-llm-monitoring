package org

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/emrah/mf-backend/internal/common"
)

const (
	defaultActivityLimit = 50
	maxActivityLimit     = 100
)

// Activity is GET /org/activity?limit=&before=. Metadata only: member joins,
// analysis completion / schema-invalid flags, session logins. No case text,
// transcripts, prompts, or deep links into another member's report.
func (h *Handler) Activity(w http.ResponseWriter, r *http.Request) {
	claims, ok := common.ClaimsFromContext(r.Context())
	if !ok {
		common.Error(w, common.ErrUnauthorized("authentication required"))
		return
	}

	limit := defaultActivityLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			common.Error(w, common.ErrBadRequest("limit must be a positive integer"))
			return
		}
		if n > maxActivityLimit {
			n = maxActivityLimit
		}
		limit = n
	}

	var before *time.Time
	if raw := r.URL.Query().Get("before"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			common.Error(w, common.ErrBadRequest("before must be RFC3339"))
			return
		}
		before = &t
	}

	items, err := h.store.Activity(r.Context(), claims.OrgID, limit, before)
	if err != nil {
		slog.Error("org activity failed", "error", err, "org_id", claims.OrgID)
		common.Error(w, common.ErrInternal("could not read activity"))
		return
	}
	if items == nil {
		items = []ActivityItem{}
	}
	common.JSON(w, http.StatusOK, ActivityResponse{Items: items})
}
