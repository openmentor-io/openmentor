package jwt

import (
	"errors"
	"testing"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
)

// The session cookie is the only credential the portal and the admin panel have,
// and until H2 this package had no tests at all. These pin the contract itself —
// what is required, and what is refused — rather than any one caller's use of it.
//
// No test hard-codes a signature or a token string: everything is minted here, so
// nothing in this file is a secret and none of it needs rotating.

const (
	testSecret = "test-secret-not-a-real-one-0123456789abcdef"
	testIssuer = "openmentor-api-test"
	testUUID   = "3f2504e0-4f89-11d3-9a0c-0305e82c3301"
	otherUUID  = "6ba7b810-9dad-11d1-80b4-00c04fd430c8"
)

func testManager() *TokenManager { return NewTokenManager(testSecret, testIssuer, 24) }

// signClaims mints a token with arbitrary claims and an arbitrary algorithm,
// which is how the rejection cases construct tokens the issuer would never emit.
func signClaims(t *testing.T, method gojwt.SigningMethod, claims MentorClaims) string {
	t.Helper()
	signed, err := gojwt.NewWithClaims(method, claims).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("sign test token: %v", err)
	}
	return signed
}

// validMentorClaims is the shape a mentor token has today; each rejection case
// starts from it and breaks exactly one thing.
func validMentorClaims() MentorClaims {
	now := time.Now()
	return MentorClaims{
		MentorUUID:     testUUID,
		LegacyID:       42,
		Name:           "Test Mentor",
		TokenType:      TokenTypeMentor,
		SessionVersion: 3,
		RegisteredClaims: gojwt.RegisteredClaims{
			ExpiresAt: gojwt.NewNumericDate(now.Add(time.Hour)),
			IssuedAt:  gojwt.NewNumericDate(now),
			NotBefore: gojwt.NewNumericDate(now),
			Issuer:    testIssuer,
			Subject:   testUUID,
			Audience:  gojwt.ClaimStrings{AudienceMentor},
		},
	}
}

func TestGeneratedMentorTokenRoundTrips(t *testing.T) {
	tm := testManager()

	token, err := tm.GenerateToken(testUUID, 42, "mentor@example.test", "Test Mentor", 7)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	claims, err := tm.ValidateMentorToken(token)
	if err != nil {
		t.Fatalf("ValidateMentorToken on a freshly minted token: %v", err)
	}
	if claims.MentorUUID != testUUID || claims.Subject != testUUID {
		t.Errorf("subject/mentor_uuid = %q/%q, want %q", claims.Subject, claims.MentorUUID, testUUID)
	}
	if claims.SessionVersion != 7 {
		t.Errorf("session_version = %d, want 7 — revocation cannot work without it", claims.SessionVersion)
	}
	if claims.TokenType != TokenTypeMentor {
		t.Errorf("token_type = %q, want %q", claims.TokenType, TokenTypeMentor)
	}
}

func TestGeneratedAdminTokenRoundTrips(t *testing.T) {
	tm := testManager()

	token, err := tm.GenerateTokenWithRole(testUUID, 0, "mod@example.test", "Mod", "admin", 2)
	if err != nil {
		t.Fatalf("GenerateTokenWithRole: %v", err)
	}

	claims, err := tm.ValidateAdminToken(token)
	if err != nil {
		t.Fatalf("ValidateAdminToken on a freshly minted token: %v", err)
	}
	if claims.Role != "admin" || claims.SessionVersion != 2 {
		t.Errorf("role/session_version = %q/%d, want admin/2", claims.Role, claims.SessionVersion)
	}
}

// TestRejectsAnotherHMACAlgorithmWithTheSameSecret is the H2 headline: validating
// the HMAC *family* accepted HS384 and HS512 signed with the same key, so an
// attacker who could influence the header chose the algorithm. Only HS256 is
// issued, so only HS256 is accepted.
func TestRejectsAnotherHMACAlgorithmWithTheSameSecret(t *testing.T) {
	for _, method := range []gojwt.SigningMethod{gojwt.SigningMethodHS384, gojwt.SigningMethodHS512} {
		t.Run(method.Alg(), func(t *testing.T) {
			token := signClaims(t, method, validMentorClaims())

			if _, err := testManager().ValidateMentorToken(token); err == nil {
				t.Fatalf("%s token signed with the same secret was accepted", method.Alg())
			}
		})
	}
}

func TestRejectsAForeignIssuer(t *testing.T) {
	claims := validMentorClaims()
	claims.Issuer = "someone-elses-issuer"

	if _, err := testManager().ValidateMentorToken(signClaims(t, gojwt.SigningMethodHS256, claims)); err == nil {
		t.Fatal("a token from a foreign issuer was accepted")
	}
}

// TestRejectsTheOtherRealmsAudience: mentor and admin sessions are signed with the
// SAME key, so the audience (and token_type) is the only thing keeping an admin
// token from being spent as a mentor one and vice versa.
func TestRejectsTheOtherRealmsAudience(t *testing.T) {
	tm := testManager()

	mentorToken, err := tm.GenerateToken(testUUID, 1, "", "M", 1)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	adminToken, err := tm.GenerateTokenWithRole(testUUID, 0, "", "A", "admin", 1)
	if err != nil {
		t.Fatalf("GenerateTokenWithRole: %v", err)
	}

	if _, err := tm.ValidateAdminToken(mentorToken); err == nil {
		t.Error("a mentor token was accepted as an admin session")
	}
	if _, err := tm.ValidateMentorToken(adminToken); err == nil {
		t.Error("an admin token was accepted as a mentor session")
	}
}

func TestRejectsAnUnknownAudience(t *testing.T) {
	claims := validMentorClaims()
	claims.Audience = gojwt.ClaimStrings{"some-other-service"}

	if _, err := testManager().ValidateMentorToken(signClaims(t, gojwt.SigningMethodHS256, claims)); err == nil {
		t.Fatal("a token minted for a foreign audience was accepted")
	}
}

// TestMissingRequiredTimeClaimsAreRejected: the session middlewares dereference
// ExpiresAt and IssuedAt to build the session object, so a token missing either
// used to surface as a recovered 500 instead of a 401.
func TestMissingRequiredTimeClaimsAreRejected(t *testing.T) {
	cases := map[string]func(*MentorClaims){
		"no exp": func(c *MentorClaims) { c.ExpiresAt = nil },
		"no iat": func(c *MentorClaims) { c.IssuedAt = nil },
	}

	for name, breakIt := range cases {
		t.Run(name, func(t *testing.T) {
			claims := validMentorClaims()
			breakIt(&claims)

			got, err := testManager().ValidateMentorToken(signClaims(t, gojwt.SigningMethodHS256, claims))
			if err == nil {
				t.Fatalf("token with %s was accepted (claims: %+v)", name, got)
			}
		})
	}
}

func TestRejectsAnInconsistentOrNonUUIDSubject(t *testing.T) {
	cases := map[string]func(*MentorClaims){
		"subject is not a UUID":       func(c *MentorClaims) { c.Subject, c.MentorUUID = "42", "42" },
		"subject is empty":            func(c *MentorClaims) { c.Subject = "" },
		"mentor_uuid disagrees":       func(c *MentorClaims) { c.MentorUUID = otherUUID },
		"mentor_uuid is not set":      func(c *MentorClaims) { c.MentorUUID = "" },
		"subject has a trailing char": func(c *MentorClaims) { c.Subject += "0" },
	}

	for name, breakIt := range cases {
		t.Run(name, func(t *testing.T) {
			claims := validMentorClaims()
			breakIt(&claims)

			if _, err := testManager().ValidateMentorToken(signClaims(t, gojwt.SigningMethodHS256, claims)); err == nil {
				t.Fatalf("token where %s was accepted", name)
			}
		})
	}
}

func TestExpiredTokenReportsExpiry(t *testing.T) {
	claims := validMentorClaims()
	claims.ExpiresAt = gojwt.NewNumericDate(time.Now().Add(-time.Minute))
	claims.IssuedAt = gojwt.NewNumericDate(time.Now().Add(-time.Hour))
	claims.NotBefore = claims.IssuedAt

	_, err := testManager().ValidateMentorToken(signClaims(t, gojwt.SigningMethodHS256, claims))
	if !errors.Is(err, ErrExpiredToken) {
		// The middleware branches on this to say "Session expired" rather than a
		// bare "Unauthorized", which is what tells the client to re-login.
		t.Fatalf("error = %v, want ErrExpiredToken", err)
	}
}

// TestClockSkewLeewayIsBoundedAt30s pins both halves of the leeway: a token
// minted a few seconds in the "future" by a host whose clock runs ahead must
// validate (otherwise a multi-host deploy 401s people right after login), and a
// token past its expiry by more than the leeway must still be refused. The
// second half is what stops the leeway from being widened into a real extension
// of the 24h session.
func TestClockSkewLeewayIsBoundedAt30s(t *testing.T) {
	cases := map[string]struct {
		exp, iat time.Duration // offsets from now; nbf follows iat, as minting does
		wantErr  error         // nil means "must validate"
	}{
		"minted 20s ahead by a faster clock": {exp: 24 * time.Hour, iat: 20 * time.Second, wantErr: nil},
		"minted 5m ahead is not skew":        {exp: 24 * time.Hour, iat: 5 * time.Minute, wantErr: ErrInvalidToken},
		"expired 10s ago, inside the leeway": {exp: -10 * time.Second, iat: -time.Hour, wantErr: nil},
		"expired 45s ago":                    {exp: -45 * time.Second, iat: -time.Hour, wantErr: ErrExpiredToken},
		"expired 1h ago":                     {exp: -time.Hour, iat: -2 * time.Hour, wantErr: ErrExpiredToken},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			now := time.Now()
			claims := validMentorClaims()
			claims.ExpiresAt = gojwt.NewNumericDate(now.Add(tc.exp))
			claims.IssuedAt = gojwt.NewNumericDate(now.Add(tc.iat))
			claims.NotBefore = claims.IssuedAt

			_, err := testManager().ValidateMentorToken(signClaims(t, gojwt.SigningMethodHS256, claims))
			switch {
			case tc.wantErr == nil && err != nil:
				t.Fatalf("token was rejected: %v", err)
			case tc.wantErr != nil && !errors.Is(err, tc.wantErr):
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestRejectsATokenSignedWithAnotherSecret(t *testing.T) {
	other := NewTokenManager("a-completely-different-secret-0123456789", testIssuer, 24)
	token, err := other.GenerateToken(testUUID, 1, "", "M", 1)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	if _, err := testManager().ValidateMentorToken(token); err == nil {
		t.Fatal("a token signed with a different secret was accepted")
	}
}

// TestPreD57TokensStayValid is the operational half of the contract: tightening
// it must not log out the sessions that are already live. Tokens minted before
// this change carry no `aud` and no session_version, and pre-M13 ones carry no
// token_type either.
//
// Delete this test together with the legacy branches in validate/validateTokenType
// once one session TTL has passed since the deploy.
func TestPreD57TokensStayValid(t *testing.T) {
	tm := testManager()

	t.Run("mentor token without aud", func(t *testing.T) {
		claims := validMentorClaims()
		claims.Audience = nil
		claims.SessionVersion = 0

		if _, err := tm.ValidateMentorToken(signClaims(t, gojwt.SigningMethodHS256, claims)); err != nil {
			t.Fatalf("a live pre-D57 mentor session was rejected: %v", err)
		}
	})

	t.Run("pre-M13 mentor token without token_type", func(t *testing.T) {
		claims := validMentorClaims()
		claims.Audience = nil
		claims.TokenType = ""
		claims.SessionVersion = 0

		if _, err := tm.ValidateMentorToken(signClaims(t, gojwt.SigningMethodHS256, claims)); err != nil {
			t.Fatalf("a live pre-M13 mentor session was rejected: %v", err)
		}
	})

	t.Run("pre-M13 admin token without token_type", func(t *testing.T) {
		claims := validMentorClaims()
		claims.Audience = nil
		claims.TokenType = ""
		claims.Role = "admin"
		claims.SessionVersion = 0

		if _, err := tm.ValidateAdminToken(signClaims(t, gojwt.SigningMethodHS256, claims)); err != nil {
			t.Fatalf("a live pre-M13 admin session was rejected: %v", err)
		}
	})

	// ...and the legacy allowance must not become a hole: a token with no
	// token_type but a role is an ADMIN token, so the mentor realm still refuses
	// it even though both realms share the signing key.
	t.Run("a roled legacy token is not a mentor token", func(t *testing.T) {
		claims := validMentorClaims()
		claims.Audience = nil
		claims.TokenType = ""
		claims.Role = "admin"

		if _, err := tm.ValidateMentorToken(signClaims(t, gojwt.SigningMethodHS256, claims)); err == nil {
			t.Fatal("a legacy admin token was accepted as a mentor session")
		}
	})
}

func TestIsUUIDShape(t *testing.T) {
	valid := []string{testUUID, otherUUID, "00000000-0000-0000-0000-000000000000",
		"3F2504E0-4F89-11D3-9A0C-0305E82C3301"}
	invalid := []string{"", "42", testUUID + "x", testUUID[:35],
		"3f2504e0x4f89-11d3-9a0c-0305e82c3301", "3f2504e0-4f89-11d3-9a0c-0305e82c330g"}

	for _, s := range valid {
		if !isUUID(s) {
			t.Errorf("isUUID(%q) = false, want true", s)
		}
	}
	for _, s := range invalid {
		if isUUID(s) {
			t.Errorf("isUUID(%q) = true, want false", s)
		}
	}
}

func TestTimingSafeCompare(t *testing.T) {
	if !TimingSafeCompare("abc", "abc") {
		t.Error("equal strings compared unequal")
	}
	if TimingSafeCompare("abc", "abd") || TimingSafeCompare("abc", "abcd") {
		t.Error("unequal strings compared equal")
	}
}
