package org

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/emrah/mf-backend/internal/common"
	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/bcrypt"
)

// ListMembers returns every user in claims.OrgID. No client-supplied org id.
func (h *Handler) ListMembers(w http.ResponseWriter, r *http.Request) {
	claims, ok := common.ClaimsFromContext(r.Context())
	if !ok {
		common.Error(w, common.ErrUnauthorized("authentication required"))
		return
	}

	members, err := h.store.ListMembers(r.Context(), claims.OrgID)
	if err != nil {
		slog.Error("org list members failed", "error", err, "org_id", claims.OrgID)
		common.Error(w, common.ErrInternal("could not list members"))
		return
	}
	if members == nil {
		members = []Member{}
	}
	common.JSON(w, http.StatusOK, ListMembersResponse{Members: members})
}

// CreateMember adds a seat under claims.OrgID with a one-time temporary
// password. Seat_limit is a ceiling checked before insert; raising it is a
// platform-admin concern, not this panel's.
func (h *Handler) CreateMember(w http.ResponseWriter, r *http.Request) {
	claims, ok := common.ClaimsFromContext(r.Context())
	if !ok {
		common.Error(w, common.ErrUnauthorized("authentication required"))
		return
	}

	var req CreateMemberRequest
	if err := common.Decode(r, &req); err != nil {
		common.Error(w, err)
		return
	}
	if err := req.Normalize(); err != nil {
		common.Error(w, common.ErrBadRequest(err.Error()))
		return
	}

	summary, err := h.store.GetOrgSummary(r.Context(), claims.OrgID)
	if errors.Is(err, ErrNoRows) {
		common.Error(w, common.ErrNotFound("organization not found"))
		return
	}
	if err != nil {
		slog.Error("org create member: summary failed", "error", err, "org_id", claims.OrgID)
		common.Error(w, common.ErrInternal("could not load organization"))
		return
	}
	if summary.MemberCount >= summary.SeatLimit {
		common.Error(w, common.ErrConflict("seat limit reached"))
		return
	}

	temp, err := temporaryPassword()
	if err != nil {
		common.Error(w, common.ErrInternal("could not create temporary password"))
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(temp), h.bcryptCost)
	if err != nil {
		common.Error(w, common.ErrInternal("could not hash temporary password"))
		return
	}

	member, err := h.store.CreateMember(r.Context(), claims.OrgID, req.Name, req.Email, req.OrgRole, string(hash))
	if err != nil {
		if isUniqueViolation(err) {
			common.Error(w, common.ErrConflict("email already registered"))
			return
		}
		slog.Error("org create member failed", "error", err, "org_id", claims.OrgID)
		common.Error(w, common.ErrInternal("could not create member"))
		return
	}

	slog.Info("org member created", "org_id", claims.OrgID, "member_id", member.ID, "org_role", member.OrgRole)
	h.recordAudit(r.Context(), claims.UserID, "org.member.create", member.ID, map[string]any{
		"org_role": member.OrgRole,
	})
	common.JSON(w, http.StatusCreated, CreateMemberResponse{
		Member:            member,
		TemporaryPassword: temp,
	})
}

func (r *CreateMemberRequest) Normalize() error {
	r.Name = strings.TrimSpace(r.Name)
	r.Email = strings.ToLower(strings.TrimSpace(r.Email))
	r.OrgRole = strings.TrimSpace(r.OrgRole)

	if r.Name == "" {
		return errors.New("name is required")
	}
	if r.Email == "" || !strings.Contains(r.Email, "@") {
		return errors.New("valid email is required")
	}
	switch r.OrgRole {
	case "admin", "member":
		return nil
	case "owner":
		return errors.New("org_role cannot be owner")
	default:
		return errors.New("org_role must be 'admin' or 'member'")
	}
}

// SetMemberRole changes a seat to admin|member. Target must sit in
// claims.OrgID (else 404); owner is immutable (400). Role is re-read from
// the row so a stale JWT cannot demote or delete after a race.
func (h *Handler) SetMemberRole(w http.ResponseWriter, r *http.Request) {
	claims, ok := common.ClaimsFromContext(r.Context())
	if !ok {
		common.Error(w, common.ErrUnauthorized("authentication required"))
		return
	}

	var req SetMemberRoleRequest
	if err := common.Decode(r, &req); err != nil {
		common.Error(w, err)
		return
	}
	req.OrgRole = strings.TrimSpace(req.OrgRole)
	switch req.OrgRole {
	case "admin", "member":
	default:
		common.Error(w, common.ErrBadRequest("org_role must be 'admin' or 'member'"))
		return
	}

	id := chi.URLParam(r, "id")
	if _, err := h.loadMutableMember(r.Context(), claims.OrgID, id); err != nil {
		common.Error(w, err)
		return
	}

	member, err := h.store.SetMemberRole(r.Context(), id, req.OrgRole)
	if errors.Is(err, ErrNoRows) {
		common.Error(w, common.ErrNotFound("member not found"))
		return
	}
	if err != nil {
		slog.Error("org set member role failed", "error", err, "org_id", claims.OrgID, "member_id", id)
		common.Error(w, common.ErrInternal("could not update member role"))
		return
	}

	slog.Info("org member role changed", "org_id", claims.OrgID, "member_id", id, "org_role", member.OrgRole)
	h.recordAudit(r.Context(), claims.UserID, "org.member.role", id, map[string]any{
		"org_role": member.OrgRole,
	})
	common.JSON(w, http.StatusOK, member)
}

// DeleteMember hard-deletes a seat in claims.OrgID. Owner is refused with
// 400; a UUID from another org is 404.
func (h *Handler) DeleteMember(w http.ResponseWriter, r *http.Request) {
	claims, ok := common.ClaimsFromContext(r.Context())
	if !ok {
		common.Error(w, common.ErrUnauthorized("authentication required"))
		return
	}

	id := chi.URLParam(r, "id")
	target, err := h.loadMutableMember(r.Context(), claims.OrgID, id)
	if err != nil {
		common.Error(w, err)
		return
	}

	if err := h.store.DeleteMember(r.Context(), id); err != nil {
		if errors.Is(err, ErrNoRows) {
			common.Error(w, common.ErrNotFound("member not found"))
			return
		}
		slog.Error("org delete member failed", "error", err, "org_id", claims.OrgID, "member_id", id)
		common.Error(w, common.ErrInternal("could not delete member"))
		return
	}

	slog.Info("org member deleted", "org_id", claims.OrgID, "member_id", id)
	h.recordAudit(r.Context(), claims.UserID, "org.member.remove", id, map[string]any{
		"org_role": target.OrgRole,
	})
	w.WriteHeader(http.StatusNoContent)
}

// loadMutableMember re-reads the target row and enforces tenant + owner rules.
// Cross-org and missing ids both surface as 404 so probing reveals nothing.
func (h *Handler) loadMutableMember(ctx context.Context, actorOrgID, userID string) (Member, error) {
	m, orgID, err := h.store.GetMember(ctx, userID)
	if errors.Is(err, ErrNoRows) {
		return Member{}, common.ErrNotFound("member not found")
	}
	if err != nil {
		slog.Error("org load member failed", "error", err, "org_id", actorOrgID, "member_id", userID)
		return Member{}, common.ErrInternal("could not load member")
	}
	if orgID != actorOrgID {
		return Member{}, common.ErrNotFound("member not found")
	}
	if m.OrgRole == "owner" {
		return Member{}, common.ErrBadRequest("cannot modify organization owner")
	}
	return m, nil
}

// temporaryPassword mirrors admin's helper: crypto/rand → URL-safe base64.
// Duplicated here so org does not import admin (YAGNI / import cycle risk).
func temporaryPassword() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
