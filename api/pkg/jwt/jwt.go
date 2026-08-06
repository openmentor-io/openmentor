package jwt

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var (
	ErrInvalidToken = errors.New("invalid token")
	ErrExpiredToken = errors.New("token has expired")
	ErrInvalidClaim = errors.New("invalid token claims")
)

// Token type values, carried in the token_type claim to bind a token to an
// audience. SECURITY: prevents a moderator (admin) token from being accepted
// by the mentor session middleware and vice-versa, even though both realms are
// signed with the same key (M13).
const (
	TokenTypeMentor = "mentor"
	TokenTypeAdmin  = "admin"
)

// Audience values, carried in the standard `aud` claim. token_type already
// separates the two realms, but it is a house claim: anything that validates one
// of these tokens with an off-the-shelf JWT library sees no realm binding at all
// unless it is in `aud` (H2). Both are in the contract, and validation requires
// both.
const (
	AudienceMentor = "openmentor-mentor"
	AudienceAdmin  = "openmentor-admin"
)

// signingMethod is the ONE algorithm this service issues and accepts.
//
// Checking the HMAC *family* (which is what this used to do) blocks the
// RS256 -> HS256 confusion attack but still accepts HS384 and HS512 signed with
// the same secret, so an attacker who can influence the header picks the
// algorithm. Pinning the exact method removes the choice.
var signingMethod = jwt.SigningMethodHS256

// clockSkewLeeway is how far the validating host's clock may sit behind the
// minting host's before a freshly issued token is rejected as not-yet-valid.
// See the parser options in validate for why it is not zero, and
// TestClockSkewLeewayIsBoundedAt30s for what it must not become.
const clockSkewLeeway = 30 * time.Second

// MentorClaims represents the JWT claims for a mentor session
type MentorClaims struct {
	MentorUUID string `json:"mentor_uuid"` // Primary identifier (UUID)
	LegacyID   int    `json:"legacy_id"`   // For backwards compatibility
	Email      string `json:"email"`
	Name       string `json:"name"`
	Role       string `json:"role,omitempty"`       // Used by moderator/admin sessions
	TokenType  string `json:"token_type,omitempty"` // "mentor" | "admin" (see M13)
	// SessionVersion is the identity row's session_version at mint time. The
	// session middleware compares it against the live row, so bumping the column
	// revokes every token issued for that identity (D58). Zero means "minted
	// before the claim existed" and is not version-checked — see the middleware.
	SessionVersion int `json:"session_version,omitempty"`
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

// GenerateToken creates a new JWT token for a mentor session.
func (tm *TokenManager) GenerateToken(mentorUUID string, legacyID int, email, name string, sessionVersion int) (string, error) {
	return tm.generateToken(mentorUUID, legacyID, email, name, "", TokenTypeMentor, AudienceMentor, sessionVersion)
}

// GenerateTokenWithRole creates a JWT token for a moderator/admin session with
// an explicit role claim.
func (tm *TokenManager) GenerateTokenWithRole(subjectID string, legacyID int, email, name, role string, sessionVersion int) (string, error) {
	return tm.generateToken(subjectID, legacyID, email, name, role, TokenTypeAdmin, AudienceAdmin, sessionVersion)
}

func (tm *TokenManager) generateToken(subjectID string, legacyID int, email, name, role, tokenType, audience string, sessionVersion int) (string, error) {
	now := time.Now()
	expiresAt := now.Add(tm.ttl)

	claims := MentorClaims{
		MentorUUID:     subjectID,
		LegacyID:       legacyID,
		Email:          email,
		Name:           name,
		Role:           role,
		TokenType:      tokenType,
		SessionVersion: sessionVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			Issuer:    tm.issuer,
			Subject:   subjectID, // UUID as subject
			Audience:  jwt.ClaimStrings{audience},
		},
	}

	token := jwt.NewWithClaims(signingMethod, claims)
	signedToken, err := token.SignedString(tm.secret)
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}

	return signedToken, nil
}

// ValidateMentorToken validates a mentor session token and returns its claims.
func (tm *TokenManager) ValidateMentorToken(tokenString string) (*MentorClaims, error) {
	return tm.validate(tokenString, TokenTypeMentor, AudienceMentor)
}

// ValidateAdminToken validates a moderator/admin session token and returns its
// claims. Callers must still check the role — this only establishes that the
// token was minted for the admin realm.
func (tm *TokenManager) ValidateAdminToken(tokenString string) (*MentorClaims, error) {
	return tm.validate(tokenString, TokenTypeAdmin, AudienceAdmin)
}

// validate parses a token and enforces the whole contract: exact algorithm,
// issuer, audience, expiry, issued-at, a UUID subject consistent with the
// mentor_uuid claim, and the realm's token type.
//
// There is deliberately NO realm-agnostic entry point. A single ValidateToken
// that left the realm to the caller is what let the two middlewares each
// re-derive the rule, and only one of them checked the role.
func (tm *TokenManager) validate(tokenString, wantType, wantAudience string) (*MentorClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &MentorClaims{},
		func(*jwt.Token) (interface{}, error) { return tm.secret, nil },
		// WithValidMethods pins the algorithm before the signature is checked,
		// so the keyfunc above never has to inspect the header itself.
		jwt.WithValidMethods([]string{signingMethod.Alg()}),
		jwt.WithIssuer(tm.issuer),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		// Every minted token carries nbf = iat = now, so with zero leeway a token
		// minted on a host whose clock is a second ahead is "not yet valid" on a
		// host that is a second behind — an intermittent 401 immediately after
		// login, the first time the API runs on more than one machine. 30s is the
		// usual NTP-corrected drift ceiling and stays four orders of magnitude
		// below the 24h TTL, so it cannot meaningfully extend a session's life.
		jwt.WithLeeway(clockSkewLeeway),
	)
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

	// Presence checks the parser does not make. Both are dereferenced by the
	// session middlewares to build the session object, where a nil would have
	// surfaced as a recovered 500 rather than a 401.
	if claims.ExpiresAt == nil || claims.IssuedAt == nil {
		return nil, fmt.Errorf("%w: exp and iat are required", ErrInvalidClaim)
	}

	if err := validateAudience(claims, wantAudience); err != nil {
		return nil, err
	}

	// The subject must be a UUID and must agree with mentor_uuid, which is what
	// every consumer actually reads. Two identifier claims allowed to disagree
	// are one bug away from an authorization decision made on the wrong one.
	if !isUUID(claims.Subject) {
		return nil, fmt.Errorf("%w: subject is not a UUID", ErrInvalidClaim)
	}
	if claims.MentorUUID != claims.Subject {
		return nil, fmt.Errorf("%w: mentor_uuid does not match subject", ErrInvalidClaim)
	}

	return claims, validateTokenType(claims, wantType)
}

// validateAudience rejects a token minted for the other realm.
//
// jwt.WithAudience would REQUIRE the claim, which would invalidate every session
// issued before D57 the moment this deploys. So a WRONG audience is rejected and
// a MISSING one falls through to the token_type check, which every live token
// does carry (M13).
//
// Remove the empty-audience branch once one session TTL (24h) has passed since
// the deploy — at that point no token without `aud` can still be valid — and
// replace this with jwt.WithAudience in the parser options. Tracked in issue #80
// with the rest of the D57/D58 compatibility batch; it is one deletion PR.
func validateAudience(claims *MentorClaims, wantAudience string) error {
	aud, err := claims.GetAudience()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidClaim, err)
	}
	if len(aud) > 0 && !slices.Contains(aud, wantAudience) {
		return fmt.Errorf("%w: audience %v is not %q", ErrInvalidClaim, aud, wantAudience)
	}
	return nil
}

// validateTokenType enforces the realm binding, including for the pre-M13 tokens
// that carry no token_type at all.
//
// An empty token_type means "minted before the claim existed" and is treated as
// a mentor token, which is the rule the mentor middleware already applied. A
// legacy ADMIN token stays distinguishable even then, because mentor tokens never
// carry a role — so an empty token_type WITH a role is rejected in the mentor
// realm rather than waved through.
//
// Remove the legacy branches together with the empty-audience one above (#80).
func validateTokenType(claims *MentorClaims, wantType string) error {
	switch {
	case claims.TokenType == wantType:
		return nil
	case claims.TokenType != "":
		return fmt.Errorf("%w: token_type %q is not %q", ErrInvalidClaim, claims.TokenType, wantType)
	case wantType == TokenTypeMentor && claims.Role == "":
		return nil // pre-M13 mentor token
	case wantType == TokenTypeAdmin && claims.Role != "":
		return nil // pre-M13 admin token; the caller still validates the role
	default:
		return fmt.Errorf("%w: no token_type for the %s realm", ErrInvalidClaim, wantType)
	}
}

// isUUID reports whether s has the canonical 8-4-4-4-12 hex UUID shape.
// Hand-rolled rather than pulled from a UUID package because the only question
// here is shape: the value is a database key, never parsed into bytes.
func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
			continue
		}
		isHex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
		if !isHex {
			return false
		}
	}
	return true
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
