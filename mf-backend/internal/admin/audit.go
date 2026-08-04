package admin

import (
	"net/http"
	"strconv"

	"github.com/emrah/mf-backend/internal/common"
)

// ListAudit GET /admin/audit?page=&limit=
func (h *Handler) ListAudit(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	res, err := h.audit.ListAudit(r.Context(), page, limit)
	if err != nil {
		common.Error(w, common.ErrInternal("could not list audit log"))
		return
	}
	common.JSON(w, http.StatusOK, res)
}
