package handlers

// Username endpoints (D29). Public wording is "username"; internally it is
// mentors.slug. The change flow is deliberately separate from the regular
// profile-save endpoints: changing a username is a breaking action (shared
// links, cached OG cards) and gets its own confirmation UX + cooldown.

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/openmentor-io/openmentor/api/internal/middleware"
	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/internal/services"
	"github.com/openmentor-io/openmentor/api/pkg/slug"
)

// UsernameHandler exposes availability checks and the username change flow.
type UsernameHandler struct {
	service *services.UsernameService
}

// NewUsernameHandler creates a UsernameHandler.
func NewUsernameHandler(service *services.UsernameService) *UsernameHandler {
	return &UsernameHandler{service: service}
}

type changeUsernameRequest struct {
	Username string `json:"username" binding:"required,max=100"`
}

// respondChangeError maps service errors from Change() onto HTTP responses.
func respondChangeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, services.ErrUsernameTaken):
		c.JSON(http.StatusConflict, gin.H{"error": "This username is already taken", "reason": "taken"})
	case errors.Is(err, slug.ErrUsernameReserved):
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error(), "reason": "reserved"})
	case errors.Is(err, slug.ErrUsernameTooShort),
		errors.Is(err, slug.ErrUsernameTooLong),
		errors.Is(err, slug.ErrUsernameBadFormat):
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error(), "reason": "invalid"})
	default:
		var cooldown *services.CooldownError
		if errors.As(err, &cooldown) {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":        "Username was changed recently — please wait before changing it again",
				"reason":       "cooldown",
				"nextChangeAt": cooldown.NextChangeAt,
			})
			return
		}
		respondError(c, http.StatusInternalServerError, "Failed to change username", err)
	}
}

// PublicAvailability handles GET /api/v1/username/availability?u=<candidate>.
// Used by the registration form's live check; unauthenticated.
func (h *UsernameHandler) PublicAvailability(c *gin.Context) {
	h.respondAvailability(c, "")
}

// MentorAvailability handles GET /api/v1/mentor/username/availability?u=.
// Session-authenticated: the mentor's own current/retired usernames read as
// available (keeping or reclaiming them is allowed).
func (h *UsernameHandler) MentorAvailability(c *gin.Context) {
	session, err := middleware.GetMentorSession(c)
	if err != nil {
		respondError(c, http.StatusUnauthorized, "Unauthorized", err)
		return
	}
	h.respondAvailability(c, session.MentorID)
}

// AdminAvailability handles GET /api/v1/admin/mentors/:id/username/availability?u=.
func (h *UsernameHandler) AdminAvailability(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	mentorID := c.Param("id")
	if mentorID == "" {
		respondError(c, http.StatusBadRequest, "Mentor ID is required", nil)
		return
	}
	h.respondAvailability(c, mentorID)
}

func (h *UsernameHandler) respondAvailability(c *gin.Context, forMentorID string) {
	candidate := c.Query("u")
	if candidate == "" {
		respondError(c, http.StatusBadRequest, "Query parameter 'u' is required", nil)
		return
	}
	result, err := h.service.CheckAvailability(c.Request.Context(), candidate, forMentorID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to check availability", err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// MentorStatus handles GET /api/v1/mentor/username — current username +
// cooldown state for the dashboard card.
func (h *UsernameHandler) MentorStatus(c *gin.Context) {
	session, err := middleware.GetMentorSession(c)
	if err != nil {
		respondError(c, http.StatusUnauthorized, "Unauthorized", err)
		return
	}
	status, err := h.service.Status(c.Request.Context(), session.MentorID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to load username status", err)
		return
	}
	c.JSON(http.StatusOK, status)
}

// MentorChange handles POST /api/v1/mentor/username — the mentor-initiated
// change (14-day cooldown enforced by the service).
func (h *UsernameHandler) MentorChange(c *gin.Context) {
	session, err := middleware.GetMentorSession(c)
	if err != nil {
		respondError(c, http.StatusUnauthorized, "Unauthorized", err)
		return
	}
	var req changeUsernameRequest
	if bindErr := c.ShouldBindJSON(&req); bindErr != nil {
		respondErrorWithDetails(c, http.StatusBadRequest, "Invalid request body", []gin.H{
			{"field": "username", "message": "username is required"},
		}, bindErr)
		return
	}
	username, err := h.service.Change(c.Request.Context(), session.MentorID, req.Username, "mentor")
	if err != nil {
		respondChangeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "username": username})
}

// AdminChange handles POST /api/v1/admin/mentors/:id/username — no cooldown,
// admin role only (matches the old admin-only slug edit permission).
func (h *UsernameHandler) AdminChange(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	mentorID := c.Param("id")
	if mentorID == "" {
		respondError(c, http.StatusBadRequest, "Mentor ID is required", nil)
		return
	}
	var req changeUsernameRequest
	if bindErr := c.ShouldBindJSON(&req); bindErr != nil {
		respondErrorWithDetails(c, http.StatusBadRequest, "Invalid request body", []gin.H{
			{"field": "username", "message": "username is required"},
		}, bindErr)
		return
	}
	username, err := h.service.Change(c.Request.Context(), mentorID, req.Username, "admin")
	if err != nil {
		respondChangeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "username": username})
}

// requireAdmin loads the admin session and enforces the 'admin' role.
// Moderators can review profiles but not rename mentors.
func (h *UsernameHandler) requireAdmin(c *gin.Context) bool {
	session, err := middleware.GetAdminSession(c)
	if err != nil {
		respondError(c, http.StatusUnauthorized, "Unauthorized", err)
		return false
	}
	if session.Role != models.ModeratorRoleAdmin {
		respondError(c, http.StatusForbidden, "Only admins can change usernames", nil)
		return false
	}
	return true
}
