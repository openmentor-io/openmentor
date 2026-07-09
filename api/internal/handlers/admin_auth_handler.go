//nolint:dupl // Admin and mentor auth handlers intentionally mirror each other with role-specific services and models.
package handlers

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/openmentor-io/openmentor-api/internal/middleware"
	"github.com/openmentor-io/openmentor-api/internal/models"
	"github.com/openmentor-io/openmentor-api/internal/services"
)

// AdminAuthHandler handles moderator/admin authentication endpoints.
type AdminAuthHandler struct {
	service services.AdminAuthServiceInterface
}

func NewAdminAuthHandler(service services.AdminAuthServiceInterface) *AdminAuthHandler {
	return &AdminAuthHandler{service: service}
}

func (h *AdminAuthHandler) RequestLogin(c *gin.Context) {
	var req models.AdminRequestLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondErrorWithDetails(c, http.StatusBadRequest, "Validation failed", []gin.H{
			{"field": "email", "message": "Invalid email format"},
		}, err)
		return
	}

	resp, err := h.service.RequestLogin(c.Request.Context(), req.Email)
	if err != nil {
		if errors.Is(err, services.ErrModeratorNotFound) {
			respondError(c, http.StatusNotFound, "Moderator not found", fmt.Errorf("email %q not found", req.Email))
			return
		}
		if errors.Is(err, services.ErrModeratorNotEligible) {
			respondError(c, http.StatusForbidden, "Login not available for this account", fmt.Errorf("moderator with email %q is not eligible", req.Email))
			return
		}
		respondError(c, http.StatusInternalServerError, "Error while sending auth link", err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *AdminAuthHandler) VerifyLogin(c *gin.Context) {
	var req models.AdminVerifyLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid token format", err)
		return
	}

	session, jwtToken, err := h.service.VerifyLogin(c.Request.Context(), req.Token)
	if err != nil {
		if errors.Is(err, services.ErrAdminInvalidLoginToken) {
			respondError(c, http.StatusUnauthorized, "Invalid token", err)
			return
		}
		if errors.Is(err, services.ErrModeratorNotEligible) {
			respondError(c, http.StatusForbidden, "Login not available for this account", err)
			return
		}
		if errors.Is(err, services.ErrAdminJWTSecretNotSet) {
			respondError(c, http.StatusInternalServerError, "Service temporarily unavailable", err)
			return
		}
		respondError(c, http.StatusInternalServerError, "Error while verifying token", err)
		return
	}

	middleware.SetAdminSessionCookie(
		c,
		jwtToken,
		h.service.GetSessionTTL(),
		h.service.GetCookieDomain(),
		h.service.GetCookieSecure(),
	)

	c.JSON(http.StatusOK, models.AdminVerifyLoginResponse{
		Success: true,
		Session: session,
	})
}

func (h *AdminAuthHandler) Logout(c *gin.Context) {
	middleware.ClearAdminSessionCookie(
		c,
		h.service.GetCookieDomain(),
		h.service.GetCookieSecure(),
	)

	c.JSON(http.StatusOK, models.AdminLogoutResponse{Success: true})
}

func (h *AdminAuthHandler) GetSession(c *gin.Context) {
	session, err := middleware.GetAdminSession(c)
	if err != nil {
		respondError(c, http.StatusUnauthorized, "Not authenticated", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"session": session,
	})
}
