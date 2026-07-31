package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/openmentor-io/openmentor/api/internal/middleware"
	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/internal/services"
)

// AdminMentorRequestsHandler exposes the requests a mentor received to the
// admin moderation portal, plus the admin-only status override.
type AdminMentorRequestsHandler struct {
	service services.AdminRequestsServiceInterface
}

func NewAdminMentorRequestsHandler(service services.AdminRequestsServiceInterface) *AdminMentorRequestsHandler {
	return &AdminMentorRequestsHandler{service: service}
}

// ListMentorRequests handles GET /api/v1/admin/mentors/:id/requests
// Optional query param: status (one of the request statuses; empty = all).
func (h *AdminMentorRequestsHandler) ListMentorRequests(c *gin.Context) {
	session, mentorID, ok := h.adminContext(c)
	if !ok {
		return
	}

	statusFilter := models.RequestStatus(strings.TrimSpace(c.Query("status")))
	if statusFilter != "" && !statusFilter.IsValid() {
		respondError(c, http.StatusBadRequest, "Invalid status filter", errors.New("status must be one of: pending, contacted, working, done, declined, unavailable"))
		return
	}

	response, err := h.service.ListMentorRequests(c.Request.Context(), session, mentorID, statusFilter)
	if err != nil {
		h.respondServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, response)
}

// GetMentorRequest handles GET /api/v1/admin/mentors/:id/requests/:requestId
func (h *AdminMentorRequestsHandler) GetMentorRequest(c *gin.Context) {
	session, mentorID, ok := h.adminContext(c)
	if !ok {
		return
	}

	requestID := c.Param("requestId")
	if requestID == "" {
		respondError(c, http.StatusBadRequest, "Invalid request ID", errors.New("missing route param: requestId"))
		return
	}

	request, err := h.service.GetMentorRequest(c.Request.Context(), session, mentorID, requestID)
	if err != nil {
		h.respondServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, models.AdminClientRequestResponse{Request: request})
}

// UpdateMentorRequestStatus handles
// POST /api/v1/admin/mentors/:id/requests/:requestId/status
func (h *AdminMentorRequestsHandler) UpdateMentorRequestStatus(c *gin.Context) {
	session, mentorID, ok := h.adminContext(c)
	if !ok {
		return
	}

	requestID := c.Param("requestId")
	if requestID == "" {
		respondError(c, http.StatusBadRequest, "Invalid request ID", errors.New("missing route param: requestId"))
		return
	}

	var req models.UpdateStatusRequest
	if bindErr := c.ShouldBindJSON(&req); bindErr != nil {
		respondErrorWithDetails(c, http.StatusBadRequest, "Invalid request body", gin.H{
			"message": "Status must be one of: pending, contacted, working, done, declined, unavailable",
		}, bindErr)
		return
	}

	request, err := h.service.UpdateRequestStatus(c.Request.Context(), session, mentorID, requestID, req.Status)
	if err != nil {
		h.respondServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, models.AdminClientRequestResponse{Request: request})
}

// adminContext resolves the admin session and the :id route param, writing the
// error response itself when either is missing.
func (h *AdminMentorRequestsHandler) adminContext(c *gin.Context) (*models.AdminSession, string, bool) {
	session, err := middleware.GetAdminSession(c)
	if err != nil {
		respondError(c, http.StatusUnauthorized, "Unauthorized", err)
		return nil, "", false
	}

	mentorID := c.Param("id")
	if mentorID == "" {
		respondError(c, http.StatusBadRequest, "Invalid mentor ID", errors.New("missing route param: id"))
		return nil, "", false
	}

	return session, mentorID, true
}

func (h *AdminMentorRequestsHandler) respondServiceError(c *gin.Context, err error) {
	if errors.Is(err, services.ErrAdminForbiddenAction) {
		respondError(c, http.StatusForbidden, "Access denied", err)
		return
	}
	if errors.Is(err, services.ErrRequestNotFound) {
		respondError(c, http.StatusNotFound, "Request not found", err)
		return
	}
	if strings.Contains(strings.ToLower(err.Error()), "unsupported") {
		respondError(c, http.StatusBadRequest, "Invalid request", err)
		return
	}

	respondError(c, http.StatusInternalServerError, "Internal server error", err)
}
