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
	// MenteeSessionCookieName is the cookie used for mentee dashboard sessions
	// (D95). Separate from the mentor and admin cookies, so one browser can hold
	// any combination of the three without one logout ending another.
	MenteeSessionCookieName = "mentee_session"

	// MenteeSessionContextKey stores the authenticated mentee session in context.
	MenteeSessionContextKey = "mentee_session"
)

var (
	ErrMenteeSessionNotFound = errors.New("mentee session not found in context")
	ErrInvalidMenteeSession  = errors.New("invalid mentee session type")
)

// MenteeSessionChecker re-reads the live mentee row behind a session.
// *repository.MenteeRepository satisfies it.
type MenteeSessionChecker interface {
	MenteeSessionState(ctx context.Context, menteeID string) (sessionVersion int, err error)
}

// MenteeSessionMiddleware validates the mentee session cookie and re-reads the
// mentee row on EVERY request, reads included. Every mentee route reads the
// database anyway, so the primary-key lookup costs nothing extra, and a logout
// then revokes a copied cookie at once rather than up to 24 hours later.
//
// checker may be nil (tests with no database), which skips the re-read.
func MenteeSessionMiddleware(
	tokenManager *jwt.TokenManager, checker MenteeSessionChecker,
	cookieDomain string, cookieSecure bool,
) gin.HandlerFunc {

	return func(c *gin.Context) {
		cookie, err := c.Cookie(MenteeSessionCookieName)
		if err != nil {
			_ = c.Error(fmt.Errorf("missing mentee session cookie")) //nolint:errcheck
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			c.Abort()
			return
		}

		claims, err := tokenManager.ValidateMenteeToken(cookie)
		if err != nil {
			_ = c.Error(fmt.Errorf("invalid mentee session token: %w", err)) //nolint:errcheck
			ClearMenteeSessionCookie(c, cookieDomain, cookieSecure)
			if errors.Is(err, jwt.ErrExpiredToken) {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Session expired"})
			} else {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			}
			c.Abort()
			return
		}

		if checker != nil && !menteeSessionLive(c, checker, claims, cookieDomain, cookieSecure) {
			return
		}

		c.Set(MenteeSessionContextKey, &models.MenteeSession{
			MenteeID:  claims.MentorUUID,
			ExpiresAt: claims.ExpiresAt.Unix(),
			IssuedAt:  claims.IssuedAt.Unix(),
		})
		c.Next()
	}
}

// menteeSessionLive reports whether the session may continue. false means it
// already wrote the response and aborted.
func menteeSessionLive(
	c *gin.Context, checker MenteeSessionChecker, claims *jwt.MentorClaims,
	cookieDomain string, cookieSecure bool,
) bool {

	version, err := checker.MenteeSessionState(c.Request.Context(), claims.MentorUUID)
	switch {
	case err == nil:
	case errors.Is(err, models.ErrSessionSubjectGone):
		_ = c.Error(fmt.Errorf("mentee behind the session no longer exists")) //nolint:errcheck
		ClearMenteeSessionCookie(c, cookieDomain, cookieSecure)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		c.Abort()
		return false
	default:
		// Fail closed without clearing the cookie: the database, not the
		// session, is what failed.
		_ = c.Error(fmt.Errorf("failed to verify mentee session: %w", err)) //nolint:errcheck
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Service temporarily unavailable"})
		c.Abort()
		return false
	}

	// No grandfathering of a zero claim here, unlike sessionVersionCurrent:
	// every mentee token carries the claim.
	if claims.SessionVersion != version {
		_ = c.Error(fmt.Errorf("mentee session was revoked")) //nolint:errcheck
		ClearMenteeSessionCookie(c, cookieDomain, cookieSecure)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		c.Abort()
		return false
	}
	return true
}

func GetMenteeSession(c *gin.Context) (*models.MenteeSession, error) {
	val, exists := c.Get(MenteeSessionContextKey)
	if !exists {
		return nil, ErrMenteeSessionNotFound
	}
	session, ok := val.(*models.MenteeSession)
	if !ok {
		return nil, ErrInvalidMenteeSession
	}
	return session, nil
}

func SetMenteeSessionCookie(c *gin.Context, token string, ttlSeconds int, domain string, secure bool) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(MenteeSessionCookieName, token, ttlSeconds, "/", domain, secure, true)
}

func ClearMenteeSessionCookie(c *gin.Context, domain string, secure bool) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(MenteeSessionCookieName, "", -1, "/", domain, secure, true)
}
