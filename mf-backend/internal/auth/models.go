package auth

import "time"

// TermsVersion identifies the text a user accepted. Bump it when the wording
// changes in a way a reasonable person would want to re-read — not for typos.
const TermsVersion = "2026-08-01"

// User is the public representation of a user — note there is NO password field.
// We never serialize the password hash to JSON.
type User struct {
	ID                 string    `json:"id"`
	Email              string    `json:"email"`
	Name               string    `json:"name"`
	Role               string    `json:"role"`
	MustChangePassword bool      `json:"must_change_password"`
	OrgStatus          string    `json:"-"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`

	// Nil for an account that predates the terms, which is what the login gate
	// keys on. The accepted version is stored but not returned: nothing in the
	// product reads it yet, and a field the client cannot act on is one more
	// thing to keep in sync.
	TermsAcceptedAt *time.Time `json:"terms_accepted_at"`
}

// Session is a refresh-token record (one per login / device).
type Session struct {
	ID        string     `json:"id"`
	UserAgent string     `json:"user_agent"`
	IPAddress string     `json:"ip_address"`
	ExpiresAt time.Time  `json:"expires_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// ---- Request payloads ----

type RegisterRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
	// No omitempty and no default: an acceptance the server infers is not an
	// acceptance. Absent reads as false and is refused like an explicit false.
	AcceptedTerms bool `json:"accepted_terms"`
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type UpdateProfileRequest struct {
	Name string `json:"name"`
}

type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// ---- Response payloads ----

// TokenPair is what login/register/refresh return: a short-lived access token
// plus a long-lived refresh token.
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"` // access token lifetime, seconds
	User         User   `json:"user"`
}
