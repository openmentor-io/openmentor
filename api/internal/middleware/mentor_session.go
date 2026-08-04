package middleware

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/pkg/jwt"
)

const (
	// MentorSessionCookieName is the name of the session cookie
	MentorSessionCookieName = "mentor_session"

	// MentorSessionContextKey is the key used to store session in context
	MentorSessionContextKey = "mentor_session"
)

var (
	ErrSessionNotFound = errors.New("session not found in context")
	ErrInvalidSession  = errors.New("invalid session type")
)

// MentorSessionChecker re-reads the live mentor row behind a session.
// *repository.MentorRepository satisfies it.
type MentorSessionChecker interface {
	MentorSessionState(ctx context.Context, mentorID string) (status string, sessionVersion int, err error)
}

// MentorSessionMiddleware validates the JWT session cookie and adds the session
// to the request context.
//
// checker may be nil, which skips the live re-read (tests and tooling with no
// database). When it is set, MUTATIONS additionally verify that the mentor still
// exists, may still hold a session, and has not had their sessions revoked. Reads
// are deliberately exempt: the point is to stop a declined or logged-out mentor
// from *changing* anything, and paying a primary-key lookup on every profile GET
// buys nothing for that.
func MentorSessionMiddleware(
	tokenManager *jwt.TokenManager, checker MentorSessionChecker,
	cookieDomain string, cookieSecure bool,
) gin.HandlerFunc {

	return func(c *gin.Context) {
		// Get session cookie
		cookie, err := c.Cookie(MentorSessionCookieName)
		if err != nil {
			_ = c.Error(fmt.Errorf("missing session cookie")) //nolint:errcheck
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			c.Abort()
			return
		}

		// Validate against the mentor realm's contract: HS256 only, this issuer,
		// the mentor audience, a UUID subject and token_type "mentor" (H2). The
		// realm binding used to live here as a token_type comparison; it belongs
		// in the contract, so pkg/jwt owns it now.
		claims, err := tokenManager.ValidateMentorToken(cookie)
		if err != nil {
			_ = c.Error(fmt.Errorf("invalid session token: %w", err)) //nolint:errcheck

			// Clear invalid cookie
			clearSessionCookie(c, cookieDomain, cookieSecure)

			if errors.Is(err, jwt.ErrExpiredToken) {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Session expired"})
			} else {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			}
			c.Abort()
			return
		}

		mustRecheck := checker != nil && isMutation(c.Request.Method)
		if mustRecheck && !mentorSessionStillValid(c, checker, claims, cookieDomain, cookieSecure) {
			return
		}

		// Create session from claims
		session := &models.MentorSession{
			LegacyID:  claims.LegacyID,
			MentorID:  claims.MentorUUID,
			Email:     claims.Email,
			Name:      claims.Name,
			ExpiresAt: claims.ExpiresAt.Unix(),
			IssuedAt:  claims.IssuedAt.Unix(),
		}

		// Add session to context
		c.Set(MentorSessionContextKey, session)
		c.Next()
	}
}

// mentorSessionStillValid re-reads the mentor row and reports whether the request
// may proceed; it writes the response and aborts when it may not.
func mentorSessionStillValid(
	c *gin.Context, checker MentorSessionChecker, claims *jwt.MentorClaims,
	cookieDomain string, cookieSecure bool,
) bool {

	status, version, err := checker.MentorSessionState(c.Request.Context(), claims.MentorUUID)
	switch {
	case err == nil:
	case errors.Is(err, models.ErrSessionSubjectGone):
		_ = c.Error(fmt.Errorf("mentor behind the session no longer exists")) //nolint:errcheck
		clearSessionCookie(c, cookieDomain, cookieSecure)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		c.Abort()
		return false
	default:
		// Fail CLOSED, but do NOT clear the cookie: a database blip is not a
		// reason to log everybody out, and a 401 would make the client discard a
		// session that is still perfectly valid.
		_ = c.Error(fmt.Errorf("failed to verify mentor session: %w", err)) //nolint:errcheck
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Service temporarily unavailable"})
		c.Abort()
		return false
	}

	if !models.MayHoldMentorSession(status) {
		_ = c.Error(fmt.Errorf("mentor status %q may no longer hold a session", status)) //nolint:errcheck
		clearSessionCookie(c, cookieDomain, cookieSecure)
		c.JSON(http.StatusForbidden, gin.H{"error": "Account is not active"})
		c.Abort()
		return false
	}

	if !sessionVersionCurrent(claims.SessionVersion, version) {
		_ = c.Error(fmt.Errorf("mentor session was revoked")) //nolint:errcheck
		clearSessionCookie(c, cookieDomain, cookieSecure)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		c.Abort()
		return false
	}

	return true
}

// GetMentorSession extracts session from context
func GetMentorSession(c *gin.Context) (*models.MentorSession, error) {
	val, exists := c.Get(MentorSessionContextKey)
	if !exists {
		return nil, ErrSessionNotFound
	}

	session, ok := val.(*models.MentorSession)
	if !ok {
		return nil, ErrInvalidSession
	}

	return session, nil
}

// SetSessionCookie sets the mentor session cookie
func SetSessionCookie(c *gin.Context, token string, ttlSeconds int, domain string, secure bool) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(
		MentorSessionCookieName,
		token,
		ttlSeconds,
		"/",
		domain,
		secure,
		true, // HttpOnly
	)
}

// ClearSessionCookie clears the mentor session cookie
func ClearSessionCookie(c *gin.Context, domain string, secure bool) {
	clearSessionCookie(c, domain, secure)
}

// clearSessionCookie is an internal helper to clear the cookie
func clearSessionCookie(c *gin.Context, domain string, secure bool) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(
		MentorSessionCookieName,
		"",
		-1,
		"/",
		domain,
		secure,
		true, // HttpOnly
	)
}
