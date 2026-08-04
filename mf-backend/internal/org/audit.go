package org

import (
	"context"
	"log/slog"
)

// AuditWriter records company-panel mutations. detail must stay metadata-only
// (org_role, etc.) — never email, password, or case text.
type AuditWriter interface {
	WriteAudit(ctx context.Context, actorID, action, target string, detail map[string]any) error
}

func (h *Handler) recordAudit(ctx context.Context, actorID, action, target string, detail map[string]any) {
	if h.audit == nil {
		return
	}
	if err := h.audit.WriteAudit(ctx, actorID, action, target, detail); err != nil {
		slog.Error("audit write failed", "action", action, "target", target, "error", err)
	}
}
