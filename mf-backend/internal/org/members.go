package org

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/emrah/mf-backend/internal/common"
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

// temporaryPassword mirrors admin's helper: crypto/rand → URL-safe base64.
// Duplicated here so org does not import admin (YAGNI / import cycle risk).
func temporaryPassword() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
