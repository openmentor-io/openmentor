package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/openmentor-io/openmentor/api/internal/middleware"
	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/internal/services"
)

type AdminMentorsHandler struct {
	service services.AdminMentorsServiceInterface
}

func NewAdminMentorsHandler(service services.AdminMentorsServiceInterface) *AdminMentorsHandler {
	return &AdminMentorsHandler{service: service}
}

func (h *AdminMentorsHandler) ListMentors(c *gin.Context) {
	session, err := middleware.GetAdminSession(c)
	if err != nil {
		respondError(c, http.StatusUnauthorized, "Unauthorized", err)
		return
	}

	filter := models.MentorModerationFilter(c.DefaultQuery("status", string(models.MentorModerationFilterPending)))
	if !filter.IsValid() {
		respondError(c, http.StatusBadRequest, "Invalid status filter", errors.New("status must be pending, approved, or declined"))
		return
	}

	mentors, err := h.service.ListMentors(c.Request.Context(), session, filter)
	if err != nil {
		h.respondServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, models.AdminMentorsListResponse{
		Mentors: mentors,
		Total:   len(mentors),
	})
}

func (h *AdminMentorsHandler) GetMentor(c *gin.Context) {
	h.withAdminMentor(c, h.service.GetMentor)
}

func (h *AdminMentorsHandler) UpdateMentor(c *gin.Context) {
	session, err := middleware.GetAdminSession(c)
	if err != nil {
		respondError(c, http.StatusUnauthorized, "Unauthorized", err)
		return
	}

	mentorID := c.Param("id")
	if mentorID == "" {
		respondError(c, http.StatusBadRequest, "Invalid mentor ID", errors.New("missing route param: id"))
		return
	}

	var req models.AdminMentorProfileUpdateRequest
	if bindErr := c.ShouldBindJSON(&req); bindErr != nil {
		respondErrorWithDetails(c, http.StatusBadRequest, "Invalid request body", gin.H{"message": bindErr.Error()}, bindErr)
		return
	}

	mentor, err := h.service.UpdateMentorProfile(c.Request.Context(), session, mentorID, &req)
	if err != nil {
		h.respondServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, models.AdminMentorResponse{Mentor: mentor})
}

func (h *AdminMentorsHandler) ApproveMentor(c *gin.Context) {
	h.withAdminMentor(c, h.service.ApproveMentor)
}

func (h *AdminMentorsHandler) DeclineMentor(c *gin.Context) {
	h.withAdminMentor(c, h.service.DeclineMentor)
}

// ReturnMentor handles POST /api/v1/admin/mentors/:id/return
// Body: {reason}. Returns a pending profile to draft with a reviewer note.
func (h *AdminMentorsHandler) ReturnMentor(c *gin.Context) {
	session, err := middleware.GetAdminSession(c)
	if err != nil {
		respondError(c, http.StatusUnauthorized, "Unauthorized", err)
		return
	}

	mentorID := c.Param("id")
	if mentorID == "" {
		respondError(c, http.StatusBadRequest, "Invalid mentor ID", errors.New("missing route param: id"))
		return
	}

	var req models.AdminMentorReturnRequest
	if bindErr := c.ShouldBindJSON(&req); bindErr != nil {
		respondErrorWithDetails(c, http.StatusBadRequest, "Invalid request body", gin.H{"message": bindErr.Error()}, bindErr)
		return
	}

	mentor, err := h.service.ReturnMentor(c.Request.Context(), session, mentorID, req.Reason)
	if err != nil {
		h.respondServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, models.AdminMentorResponse{Mentor: mentor})
}

func (h *AdminMentorsHandler) withAdminMentor(
	c *gin.Context,
	action func(context.Context, *models.AdminSession, string) (*models.AdminMentorDetails, error),
) {

	session, err := middleware.GetAdminSession(c)
	if err != nil {
		respondError(c, http.StatusUnauthorized, "Unauthorized", err)
		return
	}

	mentorID := c.Param("id")
	if mentorID == "" {
		respondError(c, http.StatusBadRequest, "Invalid mentor ID", errors.New("missing route param: id"))
		return
	}

	mentor, err := action(c.Request.Context(), session, mentorID)
	if err != nil {
		h.respondServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, models.AdminMentorResponse{Mentor: mentor})
}

func (h *AdminMentorsHandler) UpdateMentorStatus(c *gin.Context) {
	session, err := middleware.GetAdminSession(c)
	if err != nil {
		respondError(c, http.StatusUnauthorized, "Unauthorized", err)
		return
	}

	mentorID := c.Param("id")
	if mentorID == "" {
		respondError(c, http.StatusBadRequest, "Invalid mentor ID", errors.New("missing route param: id"))
		return
	}

	var req models.AdminMentorStatusUpdateRequest
	if bindErr := c.ShouldBindJSON(&req); bindErr != nil {
		respondErrorWithDetails(c, http.StatusBadRequest, "Invalid request body", gin.H{"message": bindErr.Error()}, bindErr)
		return
	}

	mentor, err := h.service.UpdateMentorStatus(c.Request.Context(), session, mentorID, req.Status)
	if err != nil {
		h.respondServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, models.AdminMentorResponse{Mentor: mentor})
}

func (h *AdminMentorsHandler) UploadMentorPicture(c *gin.Context) {
	session, err := middleware.GetAdminSession(c)
	if err != nil {
		respondError(c, http.StatusUnauthorized, "Unauthorized", err)
		return
	}

	mentorID := c.Param("id")
	if mentorID == "" {
		respondError(c, http.StatusBadRequest, "Invalid mentor ID", errors.New("missing route param: id"))
		return
	}

	var req models.UploadProfilePictureRequest
	if bindErr := c.ShouldBindJSON(&req); bindErr != nil {
		respondErrorWithDetails(c, http.StatusBadRequest, "Invalid request body", gin.H{"message": bindErr.Error()}, bindErr)
		return
	}

	imageURL, err := h.service.UploadMentorPicture(c.Request.Context(), session, mentorID, &req)
	if err != nil {
		h.respondServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, models.UploadProfilePictureResponse{
		Success:  true,
		Message:  "Profile picture uploaded successfully",
		ImageURL: imageURL,
	})
}

func (h *AdminMentorsHandler) respondServiceError(c *gin.Context, err error) {
	if errors.Is(err, services.ErrAdminForbiddenAction) {
		respondError(c, http.StatusForbidden, "Access denied", err)
		return
	}

	if errors.Is(err, services.ErrMentorAlreadyActivated) {
		respondError(c, http.StatusConflict, "Mentor has already been activated and cannot be returned to draft", err)
		return
	}

	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "not found") {
		respondError(c, http.StatusNotFound, "Mentor not found", err)
		return
	}

	if strings.Contains(msg, "unsupported") || strings.Contains(msg, "required") || strings.Contains(msg, "available only") || strings.Contains(msg, "at most") {
		respondError(c, http.StatusBadRequest, "Invalid request", err)
		return
	}

	respondError(c, http.StatusInternalServerError, "Internal server error", err)
}
