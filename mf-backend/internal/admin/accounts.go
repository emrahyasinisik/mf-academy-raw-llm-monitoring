package admin

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/emrah/mf-backend/internal/common"
	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/bcrypt"
)

const (
	accountTypeIndividual = "individual"
	accountTypeCompany    = "company"

	accountStatusActive    = "active"
	accountStatusSuspended = "suspended"
)

type AccountListQuery struct {
	Q      string
	Type   string
	Status string
	Page   int
	Limit  int
}

type AccountListResult struct {
	Accounts []AccountSummary `json:"accounts"`
	Total    int              `json:"total"`
	Page     int              `json:"page"`
	Limit    int              `json:"limit"`
}

type AccountSummary struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Type            string     `json:"type"`
	TaxID           string     `json:"tax_id"`
	SeatLimit       int        `json:"seat_limit"`
	Status          string     `json:"status"`
	MemberCount     int        `json:"member_count"`
	AssessmentCount int        `json:"assessment_count"`
	LastActivityAt  *time.Time `json:"last_activity_at"`
	CreatedAt       time.Time  `json:"created_at"`
}

type AccountMember struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	OrgRole   string    `json:"org_role"`
	CreatedAt time.Time `json:"created_at"`
}

type AccountSession struct {
	ID        string    `json:"id"`
	UserAgent string    `json:"user_agent"`
	IPAddress string    `json:"ip"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

type AccountDetail struct {
	AccountSummary
	Members  []AccountMember  `json:"members"`
	Sessions []AccountSession `json:"sessions"`
}

type CreateAccountRequest struct {
	Type      string `json:"type"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	TaxID     string `json:"tax_id"`
	SeatLimit int    `json:"seat_limit"`
}

type CreateAccountResponse struct {
	Account           AccountSummary `json:"account"`
	TemporaryPassword string         `json:"temporary_password"`
	Owner             AccountMember  `json:"owner"`
}

func (r *CreateAccountRequest) Normalize() error {
	r.Type = strings.TrimSpace(r.Type)
	r.Name = strings.TrimSpace(r.Name)
	r.Email = normalizeAccountEmail(r.Email)
	r.TaxID = strings.TrimSpace(r.TaxID)

	if r.Type != accountTypeIndividual && r.Type != accountTypeCompany {
		return errors.New("type must be 'individual' or 'company'")
	}
	if r.Name == "" {
		return errors.New("name is required")
	}
	if r.Email == "" || !strings.Contains(r.Email, "@") {
		return errors.New("valid email is required")
	}
	if r.Type == accountTypeCompany && r.SeatLimit < 1 {
		r.SeatLimit = 1
	}
	if r.Type == accountTypeIndividual {
		r.SeatLimit = 1
		r.TaxID = ""
	}
	return nil
}

func normalizeAccountEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func (h *Handler) ListAccounts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	query := AccountListQuery{
		Q:      strings.TrimSpace(q.Get("q")),
		Type:   strings.TrimSpace(q.Get("type")),
		Status: strings.TrimSpace(q.Get("status")),
		Page:   clamp(atoiDefault(q.Get("page"), 1), 1, 1000000),
		Limit:  clamp(atoiDefault(q.Get("limit"), 20), 1, 100),
	}
	if query.Type != "" && query.Type != accountTypeIndividual && query.Type != accountTypeCompany {
		common.Error(w, common.ErrBadRequest("type must be 'individual' or 'company'"))
		return
	}
	if query.Status != "" && query.Status != accountStatusActive && query.Status != accountStatusSuspended {
		common.Error(w, common.ErrBadRequest("status must be 'active' or 'suspended'"))
		return
	}

	res, err := h.accounts.ListAccounts(r.Context(), query)
	if err != nil {
		common.Error(w, common.ErrInternal("could not list accounts"))
		return
	}
	common.JSON(w, http.StatusOK, res)
}

func (h *Handler) CreateAccount(w http.ResponseWriter, r *http.Request) {
	var req CreateAccountRequest
	if err := common.Decode(r, &req); err != nil {
		common.Error(w, err)
		return
	}
	if err := req.Normalize(); err != nil {
		common.Error(w, common.ErrBadRequest(err.Error()))
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

	var account AccountSummary
	var owner AccountMember
	switch req.Type {
	case accountTypeIndividual:
		account, owner, err = h.accounts.CreateIndividual(r.Context(), req.Name, req.Email, string(hash))
	case accountTypeCompany:
		account, owner, err = h.accounts.CreateCompany(r.Context(), req.Name, req.TaxID, req.SeatLimit, req.Name, req.Email, string(hash))
	}
	if err != nil {
		if isUniqueViolation(err) {
			common.Error(w, common.ErrConflict("email already registered"))
			return
		}
		common.Error(w, common.ErrInternal("could not create account"))
		return
	}

	slog.Info("admin account created", "account_id", account.ID, "type", account.Type, "owner_id", owner.ID)
	common.JSON(w, http.StatusCreated, CreateAccountResponse{
		Account:           account,
		TemporaryPassword: temp,
		Owner:             owner,
	})
}

func (h *Handler) GetAccount(w http.ResponseWriter, r *http.Request) {
	detail, err := h.accounts.GetAccount(r.Context(), chi.URLParam(r, "id"))
	if errors.Is(err, ErrNoRows) {
		common.Error(w, common.ErrNotFound("account not found"))
		return
	}
	if err != nil {
		common.Error(w, common.ErrInternal("could not read account"))
		return
	}
	common.JSON(w, http.StatusOK, detail)
}

func (h *Handler) SuspendAccount(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.accounts.SetAccountStatus(r.Context(), id, accountStatusSuspended); err != nil {
		if errors.Is(err, ErrNoRows) {
			common.Error(w, common.ErrNotFound("account not found"))
			return
		}
		common.Error(w, common.ErrInternal("could not suspend account"))
		return
	}
	members, err := h.accounts.ListMemberIDs(r.Context(), id)
	if err != nil {
		common.Error(w, common.ErrInternal("could not list account members"))
		return
	}
	for _, userID := range members {
		if _, err := h.accounts.RevokeAllSessionsForUser(r.Context(), userID); err != nil {
			common.Error(w, common.ErrInternal("could not revoke account sessions"))
			return
		}
	}
	common.JSON(w, http.StatusOK, map[string]string{"status": accountStatusSuspended})
}

func (h *Handler) UnsuspendAccount(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.accounts.SetAccountStatus(r.Context(), id, accountStatusActive); err != nil {
		if errors.Is(err, ErrNoRows) {
			common.Error(w, common.ErrNotFound("account not found"))
			return
		}
		common.Error(w, common.ErrInternal("could not unsuspend account"))
		return
	}
	common.JSON(w, http.StatusOK, map[string]string{"status": accountStatusActive})
}

func temporaryPassword() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
