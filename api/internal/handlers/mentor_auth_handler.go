//nolint:dupl // Mentor and admin auth handlers intentionally mirror each other with role-specific services and models.
package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/openmentor-io/openmentor/api/internal/middleware"
	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/internal/services"
)

// genericLoginResponse is the account-agnostic reply returned by both the
// mentor and admin request-login handlers regardless of whether the account
// exists or is eligible. SECURITY: prevents account enumeration (M3).
func genericLoginResponse() models.RequestLoginResponse {
	return models.RequestLoginResponse{
		Success: true,
		Message: models.GenericLoginMessage,
	}
}

// MentorAuthHandler handles mentor authentication endpoints
type MentorAuthHandler struct {
	service services.MentorAuthServiceInterface
}

// NewMentorAuthHandler creates a new MentorAuthHandler
func NewMentorAuthHandler(service services.MentorAuthServiceInterface) *MentorAuthHandler {
	return &MentorAuthHandler{
		service: service,
	}
}

// RequestLogin handles POST /api/v1/auth/mentor/request-login
// Generates a login token and sends it via email
func (h *MentorAuthHandler) RequestLogin(c *gin.Context) {
	var req models.RequestLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondErrorWithDetails(c, http.StatusBadRequest, "Validation failed", []gin.H{
			{"field": "email", "message": "Invalid email format"},
		}, err)
		return
	}

	resp, err := h.service.RequestLogin(c.Request.Context(), req.Email)
	if err != nil {
		// SECURITY: return an identical generic response for unknown and
		// ineligible accounts so the endpoint can't be used to enumerate
		// registered mentors. The real outcome is recorded in logs/metrics
		// by the service. Only genuine server errors surface as 5xx.
		if errors.Is(err, services.ErrMentorNotFound) || errors.Is(err, services.ErrMentorNotEligible) {
			attachError(c, err)
			c.JSON(http.StatusOK, genericLoginResponse())
			return
		}
		respondError(c, http.StatusInternalServerError, "Error while sending auth link", err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// VerifyLogin handles POST /api/v1/auth/mentor/verify
// Verifies the login token and creates a session
func (h *MentorAuthHandler) VerifyLogin(c *gin.Context) {
	var req models.VerifyLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid token format", err)
		return
	}

	session, jwtToken, err := h.service.VerifyLogin(c.Request.Context(), req.Token)
	if err != nil {
		if errors.Is(err, services.ErrInvalidLoginToken) {
			respondError(c, http.StatusUnauthorized, "Invalid token", err)
			return
		}
		if errors.Is(err, services.ErrMentorNotEligible) {
			respondError(c, http.StatusForbidden, "Login not available for this account", err)
			return
		}
		if errors.Is(err, services.ErrJWTSecretNotSet) {
			respondError(c, http.StatusInternalServerError, "Service temporarily unavailable", err)
			return
		}
		respondError(c, http.StatusInternalServerError, "Error while verifying token", err)
		return
	}

	// Set session cookie
	middleware.SetSessionCookie(
		c,
		jwtToken,
		h.service.GetSessionTTL(),
		h.service.GetCookieDomain(),
		h.service.GetCookieSecure(),
	)

	c.JSON(http.StatusOK, models.VerifyLoginResponse{
		Success: true,
		Session: session,
	})
}

// Logout handles POST /api/v1/auth/mentor/logout
// Clears the session cookie
func (h *MentorAuthHandler) Logout(c *gin.Context) {
	middleware.ClearSessionCookie(
		c,
		h.service.GetCookieDomain(),
		h.service.GetCookieSecure(),
	)

	c.JSON(http.StatusOK, models.LogoutResponse{
		Success: true,
	})
}

// GetSession handles GET /api/v1/auth/mentor/session
// Returns the current session info (for session validation)
func (h *MentorAuthHandler) GetSession(c *gin.Context) {
	session, err := middleware.GetMentorSession(c)
	if err != nil {
		respondError(c, http.StatusUnauthorized, "Not authenticated", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"session": session,
	})
}
