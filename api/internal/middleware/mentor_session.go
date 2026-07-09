package middleware

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/openmentor-io/openmentor-api/internal/models"
	"github.com/openmentor-io/openmentor-api/pkg/jwt"
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

// MentorSessionMiddleware validates JWT session cookie and adds session to context
func MentorSessionMiddleware(tokenManager *jwt.TokenManager, cookieDomain string, cookieSecure bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get session cookie
		cookie, err := c.Cookie(MentorSessionCookieName)
		if err != nil {
			_ = c.Error(fmt.Errorf("missing session cookie")) //nolint:errcheck
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			c.Abort()
			return
		}

		// Validate token
		claims, err := tokenManager.ValidateToken(cookie)
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
