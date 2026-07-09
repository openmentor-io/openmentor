package jwt

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var (
	ErrInvalidToken = errors.New("invalid token")
	ErrExpiredToken = errors.New("token has expired")
	ErrInvalidClaim = errors.New("invalid token claims")
)

// MentorClaims represents the JWT claims for a mentor session
type MentorClaims struct {
	MentorUUID string `json:"mentor_uuid"` // Primary identifier (UUID)
	LegacyID   int    `json:"legacy_id"`   // For backwards compatibility
	Email      string `json:"email"`
	Name       string `json:"name"`
	Role       string `json:"role,omitempty"` // Used by moderator/admin sessions
	jwt.RegisteredClaims
}

// TokenManager handles JWT token generation and validation
type TokenManager struct {
	secret []byte
	issuer string
	ttl    time.Duration
}

// NewTokenManager creates a new TokenManager
func NewTokenManager(secret string, issuer string, ttlHours int) *TokenManager {
	return &TokenManager{
		secret: []byte(secret),
		issuer: issuer,
		ttl:    time.Duration(ttlHours) * time.Hour,
	}
}

// GenerateToken creates a new JWT token for a mentor
func (tm *TokenManager) GenerateToken(mentorUUID string, legacyID int, email, name string) (string, error) {
	return tm.generateToken(mentorUUID, legacyID, email, name, "")
}

// GenerateTokenWithRole creates a JWT token with an explicit role claim.
func (tm *TokenManager) GenerateTokenWithRole(subjectID string, legacyID int, email, name, role string) (string, error) {
	return tm.generateToken(subjectID, legacyID, email, name, role)
}

func (tm *TokenManager) generateToken(subjectID string, legacyID int, email, name, role string) (string, error) {
	now := time.Now()
	expiresAt := now.Add(tm.ttl)

	claims := MentorClaims{
		MentorUUID: subjectID,
		LegacyID:   legacyID,
		Email:      email,
		Name:       name,
		Role:       role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			Issuer:    tm.issuer,
			Subject:   subjectID, // UUID as subject
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signedToken, err := token.SignedString(tm.secret)
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}

	return signedToken, nil
}

// ValidateToken validates a JWT token and returns the claims
func (tm *TokenManager) ValidateToken(tokenString string) (*MentorClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &MentorClaims{}, func(token *jwt.Token) (interface{}, error) {
		// Validate signing method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return tm.secret, nil
	})

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrExpiredToken
		}
		return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}

	claims, ok := token.Claims.(*MentorClaims)
	if !ok || !token.Valid {
		return nil, ErrInvalidClaim
	}

	return claims, nil
}

// GetExpirationTime returns the token expiration duration
func (tm *TokenManager) GetExpirationTime() time.Duration {
	return tm.ttl
}

// TimingSafeCompare performs a timing-safe comparison of two strings
// This prevents timing attacks when comparing tokens
func TimingSafeCompare(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
