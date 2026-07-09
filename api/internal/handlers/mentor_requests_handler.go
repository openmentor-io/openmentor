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

// MentorRequestsHandler handles mentor request management endpoints
type MentorRequestsHandler struct {
	service services.MentorRequestsServiceInterface
}

// NewMentorRequestsHandler creates a new MentorRequestsHandler
func NewMentorRequestsHandler(service services.MentorRequestsServiceInterface) *MentorRequestsHandler {
	return &MentorRequestsHandler{
		service: service,
	}
}

// GetRequests handles GET /api/v1/mentor/requests
func (h *MentorRequestsHandler) GetRequests(c *gin.Context) {
	session, err := middleware.GetMentorSession(c)
	if err != nil {
		respondError(c, http.StatusUnauthorized, "Unauthorized", err)
		return
	}

	group := c.Query("group")
	if group == "" {
		respondError(c, http.StatusBadRequest, "Missing required parameter: group", fmt.Errorf("missing required query param: group"))
		return
	}
	if group != "active" && group != "past" {
		respondError(c, http.StatusBadRequest, "Invalid group value. Must be 'active' or 'past'", fmt.Errorf("invalid group value: %q", group))
		return
	}

	response, err := h.service.GetRequests(c.Request.Context(), session.MentorID, group)
	if err != nil {
		if errors.Is(err, services.ErrInvalidRequestGroup) {
			respondError(c, http.StatusBadRequest, "Invalid request group", err)
			return
		}
		respondError(c, http.StatusInternalServerError, "Failed to fetch requests", err)
		return
	}

	c.JSON(http.StatusOK, response)
}

// GetRequestByID handles GET /api/v1/mentor/requests/:id
func (h *MentorRequestsHandler) GetRequestByID(c *gin.Context) {
	session, err := middleware.GetMentorSession(c)
	if err != nil {
		respondError(c, http.StatusUnauthorized, "Unauthorized", err)
		return
	}

	requestID := c.Param("id")
	if requestID == "" {
		respondError(c, http.StatusBadRequest, "Invalid request ID", fmt.Errorf("missing route param: id"))
		return
	}

	request, err := h.service.GetRequestByID(c.Request.Context(), session.MentorID, requestID)
	if err != nil {
		h.handleRequestError(c, err, fmt.Errorf("failed to fetch request id=%q: %w", requestID, err))
		return
	}

	c.JSON(http.StatusOK, request)
}

// UpdateStatus handles POST /api/v1/mentor/requests/:id/status
func (h *MentorRequestsHandler) UpdateStatus(c *gin.Context) {
	session, err := middleware.GetMentorSession(c)
	if err != nil {
		respondError(c, http.StatusUnauthorized, "Unauthorized", err)
		return
	}

	requestID := c.Param("id")
	if requestID == "" {
		respondError(c, http.StatusBadRequest, "Invalid request ID", fmt.Errorf("missing route param: id"))
		return
	}

	var req models.UpdateStatusRequest
	if bindErr := c.ShouldBindJSON(&req); bindErr != nil {
		respondErrorWithDetails(c, http.StatusBadRequest, "Invalid request body", gin.H{
			"message": "Status must be one of: pending, contacted, working, done, declined, unavailable",
		}, bindErr)
		return
	}

	request, err := h.service.UpdateStatus(c.Request.Context(), session.MentorID, requestID, req.Status)
	if err != nil {
		h.handleRequestError(c, err, fmt.Errorf("failed to update status for request id=%q: %w", requestID, err))
		return
	}

	c.JSON(http.StatusOK, request)
}

// DeclineRequest handles POST /api/v1/mentor/requests/:id/decline
func (h *MentorRequestsHandler) DeclineRequest(c *gin.Context) {
	session, err := middleware.GetMentorSession(c)
	if err != nil {
		respondError(c, http.StatusUnauthorized, "Unauthorized", err)
		return
	}

	requestID := c.Param("id")
	if requestID == "" {
		respondError(c, http.StatusBadRequest, "Invalid request ID", fmt.Errorf("missing route param: id"))
		return
	}

	var payload models.DeclineRequestPayload
	if bindErr := c.ShouldBindJSON(&payload); bindErr != nil {
		respondErrorWithDetails(c, http.StatusBadRequest, "Invalid request body", gin.H{
			"message": "Reason must be one of: no_time, topic_mismatch, helping_others, on_break, other",
		}, bindErr)
		return
	}

	request, err := h.service.DeclineRequest(c.Request.Context(), session.MentorID, requestID, &payload)
	if err != nil {
		h.handleRequestError(c, err, fmt.Errorf("failed to decline request id=%q: %w", requestID, err))
		return
	}

	c.JSON(http.StatusOK, request)
}

// handleRequestError maps common request service errors to HTTP responses.
func (h *MentorRequestsHandler) handleRequestError(c *gin.Context, err error, detail error) {
	attachError(c, detail)
	if errors.Is(err, services.ErrRequestNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Request not found"})
		return
	}
	if errors.Is(err, services.ErrAccessDenied) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}
	if errors.Is(err, services.ErrInvalidStatusTransition) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid status transition", "details": err.Error()})
		return
	}
	if errors.Is(err, services.ErrCannotDeclineRequest) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Cannot decline request", "details": err.Error()})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
}
