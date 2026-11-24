package handlers

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/openmentor-io/openmentor/api/internal/middleware"
	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/internal/services"
)

// MenteeServiceInterface is the mentee dashboard surface (D95).
type MenteeServiceInterface interface {
	RequestLogin(ctx context.Context, email string) (*models.RequestLoginResponse, error)
	VerifyLogin(ctx context.Context, token string) (*models.MenteeSession, string, error)
	RevokeSession(ctx context.Context, sessionToken string) error
	ListRequests(ctx context.Context, menteeID string) (*models.MenteeRequestsResponse, error)
	GetSessionTTL() int
	GetCookieDomain() string
	GetCookieSecure() bool
}

// MenteeReviewSubmitter is the one review-service method the dashboard uses.
type MenteeReviewSubmitter interface {
	SubmitReviewAsMentee(ctx context.Context, menteeID string, req *models.MenteeSubmitReviewRequest) (*models.SubmitReviewResponse, error)
}

// MenteeHandler serves the mentee dashboard: magic-link auth, the read-only
// request list, and leaving a review on a completed request.
type MenteeHandler struct {
	service MenteeServiceInterface
	reviews MenteeReviewSubmitter
}

func NewMenteeHandler(service MenteeServiceInterface, reviews MenteeReviewSubmitter) *MenteeHandler {
	return &MenteeHandler{service: service, reviews: reviews}
}

// RequestLogin handles POST /api/v1/auth/mentee/request-login. An address with
// no requests gets the same 200 as one that was sent a link (M3).
func (h *MenteeHandler) RequestLogin(c *gin.Context) {
	var req models.RequestLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondErrorWithDetails(c, http.StatusBadRequest, "Validation failed", []gin.H{
			{"field": "email", "message": "Invalid email format"},
		}, err)
		return
	}

	resp, err := h.service.RequestLogin(c.Request.Context(), req.Email)
	if err != nil {
		if errors.Is(err, services.ErrMenteeNotFound) {
			attachError(c, err)
			c.JSON(http.StatusOK, genericLoginResponse())
			return
		}
		respondError(c, http.StatusInternalServerError, "Error while sending auth link", err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// VerifyLogin handles POST /api/v1/auth/mentee/verify.
func (h *MenteeHandler) VerifyLogin(c *gin.Context) {
	var req models.VerifyLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "Invalid token format", err)
		return
	}

	session, jwtToken, err := h.service.VerifyLogin(c.Request.Context(), req.Token)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrMenteeInvalidLoginToken):
			respondError(c, http.StatusUnauthorized, "Invalid token", err)
		case errors.Is(err, services.ErrJWTSecretNotSet):
			respondError(c, http.StatusInternalServerError, "Service temporarily unavailable", err)
		default:
			respondError(c, http.StatusInternalServerError, "Error while verifying token", err)
		}
		return
	}

	middleware.SetMenteeSessionCookie(c, jwtToken, h.service.GetSessionTTL(),
		h.service.GetCookieDomain(), h.service.GetCookieSecure())
	c.JSON(http.StatusOK, models.MenteeVerifyLoginResponse{Success: true, Session: session})
}

// Logout handles POST /api/v1/auth/mentee/logout: clears the cookie and revokes
// every session issued for the mentee (D58), best effort.
func (h *MenteeHandler) Logout(c *gin.Context) {
	if cookie, err := c.Cookie(middleware.MenteeSessionCookieName); err == nil {
		if revokeErr := h.service.RevokeSession(c.Request.Context(), cookie); revokeErr != nil {
			attachError(c, revokeErr)
		}
	}
	middleware.ClearMenteeSessionCookie(c, h.service.GetCookieDomain(), h.service.GetCookieSecure())
	c.JSON(http.StatusOK, models.LogoutResponse{Success: true})
}

// GetSession handles GET /api/v1/auth/mentee/session.
func (h *MenteeHandler) GetSession(c *gin.Context) {
	session, err := middleware.GetMenteeSession(c)
	if err != nil {
		respondError(c, http.StatusUnauthorized, "Not authenticated", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "session": session})
}

// ListRequests handles GET /api/v1/mentee/requests.
func (h *MenteeHandler) ListRequests(c *gin.Context) {
	session, err := middleware.GetMenteeSession(c)
	if err != nil {
		respondError(c, http.StatusUnauthorized, "Not authenticated", err)
		return
	}
	resp, err := h.service.ListRequests(c.Request.Context(), session.MenteeID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to load requests", err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// SubmitReview handles POST /api/v1/mentee/reviews.
func (h *MenteeHandler) SubmitReview(c *gin.Context) {
	session, err := middleware.GetMenteeSession(c)
	if err != nil {
		respondError(c, http.StatusUnauthorized, "Not authenticated", err)
		return
	}

	var req models.MenteeSubmitReviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondErrorWithDetails(c, http.StatusBadRequest, "Validation failed", ParseValidationErrors(err), err)
		return
	}

	resp, err := h.reviews.SubmitReviewAsMentee(c.Request.Context(), session.MenteeID, &req)
	if err != nil {
		respondReviewSubmitError(c, resp, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}
