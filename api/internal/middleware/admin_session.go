package middleware

import (
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

// AdminSessionMiddleware validates admin JWT session cookie and stores session in context.
func AdminSessionMiddleware(tokenManager *jwt.TokenManager, cookieDomain string, cookieSecure bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		cookie, err := c.Cookie(AdminSessionCookieName)
		if err != nil {
			_ = c.Error(fmt.Errorf("missing admin session cookie")) //nolint:errcheck
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			c.Abort()
			return
		}

		claims, err := tokenManager.ValidateToken(cookie)
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

		// SECURITY: reject non-admin tokens (M13). Mentor tokens carry
		// token_type "mentor" (or empty legacy + empty role, caught below).
		if claims.TokenType == jwt.TokenTypeMentor {
			ClearAdminSessionCookie(c, cookieDomain, cookieSecure)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			c.Abort()
			return
		}

		role := models.ModeratorRole(claims.Role)
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
