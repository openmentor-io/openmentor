package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/openmentor-io/openmentor/api/internal/middleware"
	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/internal/services"
	apperrors "github.com/openmentor-io/openmentor/api/pkg/errors"
	"github.com/openmentor-io/openmentor/api/pkg/logger"
	"go.uber.org/zap"
)

// MentorProfileHandler handles session-authenticated profile endpoints
type MentorProfileHandler struct {
	mentorService  services.MentorServiceInterface
	profileService services.ProfileServiceInterface
}

// NewMentorProfileHandler creates a new MentorProfileHandler
func NewMentorProfileHandler(
	mentorService services.MentorServiceInterface,
	profileService services.ProfileServiceInterface,
) *MentorProfileHandler {

	return &MentorProfileHandler{
		mentorService:  mentorService,
		profileService: profileService,
	}
}

// GetProfile handles GET /api/v1/mentor/profile
// Returns the authenticated mentor's full profile including secure fields
func (h *MentorProfileHandler) GetProfile(c *gin.Context) {
	session, err := middleware.GetMentorSession(c)
	if err != nil {
		respondError(c, http.StatusUnauthorized, "Unauthorized", err)
		return
	}

	// AllowAnyStatus: draft/pending mentors can view their own profile
	// (it exposes status + moderationNote so they can act on a return).
	mentor, err := h.mentorService.GetMentorByMentorId(c.Request.Context(), session.MentorID, models.FilterOptions{ShowHidden: true, AllowAnyStatus: true})
	if err != nil {
		respondError(c, http.StatusNotFound, "Profile not found", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"mentor": mentor})
}

// SubmitProfile handles POST /api/v1/mentor/profile/submit
// Resubmits a returned (draft) profile for review: draft -> pending and the
// moderators are notified. Only valid while the profile is in 'draft'.
func (h *MentorProfileHandler) SubmitProfile(c *gin.Context) {
	session, err := middleware.GetMentorSession(c)
	if err != nil {
		respondError(c, http.StatusUnauthorized, "Unauthorized", err)
		return
	}

	err = h.profileService.SubmitProfileByMentorId(c.Request.Context(), session.MentorID)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrProfileNotSubmittable):
			respondError(c, http.StatusForbidden, "Only draft profiles can be submitted for review", err)
		case errors.Is(err, apperrors.ErrNotFound):
			respondError(c, http.StatusNotFound, "Profile not found", err)
		default:
			respondError(c, http.StatusInternalServerError, "Failed to submit profile", err)
		}
		return
	}

	logger.Info("Profile submitted for review via session",
		zap.String("mentor_id", session.MentorID),
		zap.String("mentor_name", session.Name))

	c.JSON(http.StatusOK, models.SubmitProfileResponse{Success: true, Status: "pending"})
}

// UpdateProfile handles POST /api/v1/mentor/profile
// Updates the authenticated mentor's profile
func (h *MentorProfileHandler) UpdateProfile(c *gin.Context) {
	session, err := middleware.GetMentorSession(c)
	if err != nil {
		respondError(c, http.StatusUnauthorized, "Unauthorized", err)
		return
	}

	var req models.SaveProfileRequest
	if bindErr := c.ShouldBindJSON(&req); bindErr != nil {
		respondErrorWithDetails(c, http.StatusBadRequest, "Invalid request body", gin.H{"message": bindErr.Error()}, bindErr)
		return
	}

	err = h.profileService.SaveProfileByMentorId(c.Request.Context(), session.MentorID, &req)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to update profile", err)
		return
	}

	logger.Info("Profile updated via session",
		zap.String("mentor_id", session.MentorID),
		zap.String("mentor_name", session.Name))

	c.JSON(http.StatusOK, models.SaveProfileResponse{Success: true})
}

// UpdateProfileStatus handles POST /api/v1/mentor/profile/status
// Toggles the authenticated mentor's catalog visibility between active and inactive
func (h *MentorProfileHandler) UpdateProfileStatus(c *gin.Context) {
	session, err := middleware.GetMentorSession(c)
	if err != nil {
		respondError(c, http.StatusUnauthorized, "Unauthorized", err)
		return
	}

	var req models.UpdateProfileStatusRequest
	if bindErr := c.ShouldBindJSON(&req); bindErr != nil {
		respondErrorWithDetails(c, http.StatusBadRequest, "Invalid request body", gin.H{"message": bindErr.Error()}, bindErr)
		return
	}

	err = h.profileService.SetProfileStatusByMentorId(c.Request.Context(), session.MentorID, req.Status)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrProfileStatusNotToggleable):
			respondError(c, http.StatusForbidden, "Only active or inactive profiles can change visibility status", err)
		case errors.Is(err, apperrors.ErrNotFound):
			respondError(c, http.StatusNotFound, "Profile not found", err)
		default:
			respondError(c, http.StatusInternalServerError, "Failed to update profile status", err)
		}
		return
	}

	logger.Info("Profile status updated via session",
		zap.String("mentor_id", session.MentorID),
		zap.String("mentor_name", session.Name),
		zap.String("status", req.Status))

	c.JSON(http.StatusOK, models.UpdateProfileStatusResponse{Success: true, Status: req.Status})
}

// UploadPicture handles POST /api/v1/mentor/profile/picture
// Uploads a new profile picture for the authenticated mentor
func (h *MentorProfileHandler) UploadPicture(c *gin.Context) {
	session, err := middleware.GetMentorSession(c)
	if err != nil {
		respondError(c, http.StatusUnauthorized, "Unauthorized", err)
		return
	}

	var req models.UploadProfilePictureRequest
	if bindErr := c.ShouldBindJSON(&req); bindErr != nil {
		respondErrorWithDetails(c, http.StatusBadRequest, "Invalid request body", gin.H{"message": bindErr.Error()}, bindErr)
		return
	}

	mentor, err := h.mentorService.GetMentorByMentorId(c.Request.Context(), session.MentorID, models.FilterOptions{ShowHidden: true, AllowAnyStatus: true})
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to fetch mentor", err)
		return
	}

	imageURL, err := h.profileService.UploadPictureByMentorId(
		c.Request.Context(),
		session.MentorID,
		mentor.Slug,
		&req,
	)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "Failed to upload picture", err)
		return
	}

	logger.Info("Profile picture uploaded via session",
		zap.String("mentor_id", session.MentorID),
		zap.String("mentor_name", session.Name),
		zap.String("image_url", imageURL))

	c.JSON(http.StatusOK, models.UploadProfilePictureResponse{
		Success:  true,
		Message:  "Profile picture uploaded successfully",
		ImageURL: imageURL,
	})
}
