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
	// AdminSessionCookieName is the cookie used for moderator/admin web sessions.
	AdminSessionCookieName = "admin_session"

	// AdminSessionContextKey stores the authenticated admin session in request context.
	AdminSessionContextKey = "admin_session"
)

var (
	ErrAdminSessionNotFound = errors.New("admin session not found in context")
	ErrInvalidAdminSession  = errors.New("invalid admin session type")
)

// AdminSessionChecker re-reads the live moderator row behind a session.
// *repository.ModeratorRepository satisfies it.
type AdminSessionChecker interface {
	ModeratorSessionState(ctx context.Context, moderatorID string) (role string, sessionVersion int, err error)
}

// AdminSessionMiddleware validates the admin JWT session cookie and stores the
// session in the request context.
//
// Unlike the mentor middleware this re-reads the moderator row on EVERY request,
// reads included: the role in the token is an authorization input, and a token is
// valid for 24 hours, so demoting or removing a moderator has to take effect on
// their next request rather than a day later. Admin traffic is a handful of
// requests, so one primary-key lookup each is free.
//
// checker may be nil, which skips the re-read (tests and tooling with no
// database); the session then carries whatever role the token claims.
func AdminSessionMiddleware(
	tokenManager *jwt.TokenManager, checker AdminSessionChecker,
	cookieDomain string, cookieSecure bool,
) gin.HandlerFunc {

	return func(c *gin.Context) {
		cookie, err := c.Cookie(AdminSessionCookieName)
		if err != nil {
			_ = c.Error(fmt.Errorf("missing admin session cookie")) //nolint:errcheck
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			c.Abort()
			return
		}

		// Validate against the admin realm's contract: HS256 only, this issuer,
		// the admin audience, a UUID subject and token_type "admin" (H2). Both
		// realms share a signing key, so the realm binding is what stops a mentor
		// token being presented here (M13) — it now lives in pkg/jwt rather than
		// being re-derived per middleware.
		claims, err := tokenManager.ValidateAdminToken(cookie)
		if err != nil {
			_ = c.Error(fmt.Errorf("invalid admin session token: %w", err)) //nolint:errcheck
			ClearAdminSessionCookie(c, cookieDomain, cookieSecure)
			if errors.Is(err, jwt.ErrExpiredToken) {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Session expired"})
			} else {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			}
			c.Abort()
			return
		}

		// The session runs with the role the DATABASE currently holds, not the one
		// the token claims. With a checker wired the two are always equal by the
		// time we get here (a mismatch aborts), but taking the live value is what
		// keeps that true if the mismatch rule is ever softened to a downgrade.
		role := models.ModeratorRole(claims.Role)
		if checker != nil {
			liveRole, ok := adminSessionRole(c, checker, claims, cookieDomain, cookieSecure)
			if !ok {
				return
			}
			role = liveRole
		}

		if !role.IsValid() {
			ClearAdminSessionCookie(c, cookieDomain, cookieSecure)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			c.Abort()
			return
		}

		session := &models.AdminSession{
			ModeratorID: claims.MentorUUID,
			Email:       claims.Email,
			Name:        claims.Name,
			Role:        role,
			ExpiresAt:   claims.ExpiresAt.Unix(),
			IssuedAt:    claims.IssuedAt.Unix(),
		}

		c.Set(AdminSessionContextKey, session)
		c.Next()
	}
}

// adminSessionRole re-reads the moderator row and returns the role the request
// should run with. ok=false means it already wrote the response and aborted.
func adminSessionRole(
	c *gin.Context, checker AdminSessionChecker, claims *jwt.MentorClaims,
	cookieDomain string, cookieSecure bool,
) (models.ModeratorRole, bool) {

	liveRole, version, err := checker.ModeratorSessionState(c.Request.Context(), claims.MentorUUID)
	switch {
	case err == nil:
	case errors.Is(err, models.ErrSessionSubjectGone):
		_ = c.Error(fmt.Errorf("moderator behind the session no longer exists")) //nolint:errcheck
		ClearAdminSessionCookie(c, cookieDomain, cookieSecure)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		c.Abort()
		return "", false
	default:
		// Fail CLOSED without clearing the cookie — see the mentor middleware.
		_ = c.Error(fmt.Errorf("failed to verify admin session: %w", err)) //nolint:errcheck
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Service temporarily unavailable"})
		c.Abort()
		return "", false
	}

	if !sessionVersionCurrent(claims.SessionVersion, version) {
		_ = c.Error(fmt.Errorf("admin session was revoked")) //nolint:errcheck
		ClearAdminSessionCookie(c, cookieDomain, cookieSecure)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		c.Abort()
		return "", false
	}

	// A role that changed at all invalidates the session rather than silently
	// running the request with different privileges: the client's UI was built
	// from the old role, and a promotion should require a fresh login just as
	// visibly as a demotion.
	if liveRole != claims.Role {
		_ = c.Error(fmt.Errorf("moderator role changed since the session was issued")) //nolint:errcheck
		ClearAdminSessionCookie(c, cookieDomain, cookieSecure)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		c.Abort()
		return "", false
	}

	return models.ModeratorRole(liveRole), true
}

func GetAdminSession(c *gin.Context) (*models.AdminSession, error) {
	val, exists := c.Get(AdminSessionContextKey)
	if !exists {
		return nil, ErrAdminSessionNotFound
	}

	session, ok := val.(*models.AdminSession)
	if !ok {
		return nil, ErrInvalidAdminSession
	}

	return session, nil
}

func SetAdminSessionCookie(c *gin.Context, token string, ttlSeconds int, domain string, secure bool) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(
		AdminSessionCookieName,
		token,
		ttlSeconds,
		"/",
		domain,
		secure,
		true,
	)
}

func ClearAdminSessionCookie(c *gin.Context, domain string, secure bool) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(
		AdminSessionCookieName,
		"",
		-1,
		"/",
		domain,
		secure,
		true,
	)
}
