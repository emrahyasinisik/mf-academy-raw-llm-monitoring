package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"

	"github.com/emrah/mf-backend/internal/common"
	"github.com/golang-jwt/jwt/v5"
)

// TokenService issues and validates JWT access tokens and opaque refresh tokens.
type TokenService struct {
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
}

func NewTokenService(secret string, accessTTL, refreshTTL time.Duration) *TokenService {
	return &TokenService{secret: []byte(secret), accessTTL: accessTTL, refreshTTL: refreshTTL}
}

// accessClaims is the JWT payload. RegisteredClaims gives us standard fields
// like expiry (exp) and subject (sub) for free.
type accessClaims struct {
	Email string `json:"email"`
	Role  string `json:"role"`
	jwt.RegisteredClaims
}

// GenerateAccess creates a signed, short-lived JWT identifying the user.
func (s *TokenService) GenerateAccess(u User) (string, time.Time, error) {
	expires := time.Now().Add(s.accessTTL)
	claims := accessClaims{
		Email: u.Email,
		Role:  u.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   u.ID,
			ExpiresAt: jwt.NewNumericDate(expires),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(s.secret)
	return signed, expires, err
}

// Verify implements common.TokenVerifier: it checks the signature and expiry
// and returns the caller's identity.
func (s *TokenService) Verify(raw string) (common.AuthClaims, error) {
	var claims accessClaims
	_, err := jwt.ParseWithClaims(raw, &claims, func(t *jwt.Token) (any, error) {
		// Reject any token not signed with the algorithm we expect —
		// this defends against the classic "alg: none" / algorithm-swap attack.
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return s.secret, nil
	})
	if err != nil {
		return common.AuthClaims{}, err
	}
	return common.AuthClaims{UserID: claims.Subject, Email: claims.Email, Role: claims.Role}, nil
}

// GenerateRefresh returns a cryptographically random opaque token plus its
// SHA-256 hash. We store only the hash in the DB, so a database leak does not
// hand an attacker usable refresh tokens.
func (s *TokenService) GenerateRefresh() (token string, hash string, expires time.Time, err error) {
	buf := make([]byte, 32)
	if _, err = rand.Read(buf); err != nil {
		return "", "", time.Time{}, err
	}
	token = base64.RawURLEncoding.EncodeToString(buf)
	hash = HashToken(token)
	expires = time.Now().Add(s.refreshTTL)
	return token, hash, expires, nil
}

// HashToken returns the hex SHA-256 of a token, used to look up refresh tokens.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// AccessTTLSeconds exposes the access-token lifetime for the API response.
func (s *TokenService) AccessTTLSeconds() int { return int(s.accessTTL.Seconds()) }
