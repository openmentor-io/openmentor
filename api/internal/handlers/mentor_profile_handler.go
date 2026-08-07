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

// DeleteProfile handles POST /api/v1/mentor/profile/delete — a mentor deleting
// their OWN profile (D70). POST rather than DELETE on /profile because the body
// carries the typed username confirmation, and a DELETE with a required body is
// a shape proxies and clients handle inconsistently.
//
// The session decides WHICH profile goes; the body only confirms the mentor
// meant it. On success every session for this mentor is already revoked, so the
// client's next request will 401 — the response says success first so the UI can
// show what happened before it gets logged out.
func (h *MentorProfileHandler) DeleteProfile(c *gin.Context) {
	session, err := middleware.GetMentorSession(c)
	if err != nil {
		respondError(c, http.StatusUnauthorized, "Unauthorized", err)
		return
	}

	var req models.DeleteProfileRequest
	if bindErr := c.ShouldBindJSON(&req); bindErr != nil {
		respondErrorWithDetails(c, http.StatusBadRequest, "Invalid request body", gin.H{"message": bindErr.Error()}, bindErr)
		return
	}

	err = h.profileService.DeleteProfileByMentorId(c.Request.Context(), session.MentorID, req.Username)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrDeleteConfirmationMismatch):
			respondError(c, http.StatusBadRequest, "The username you entered does not match your profile", err)
		case errors.Is(err, services.ErrProfileAlreadyDeleted):
			respondError(c, http.StatusConflict, "This profile has already been deleted", err)
		case errors.Is(err, apperrors.ErrNotFound):
			respondError(c, http.StatusNotFound, "Profile not found", err)
		default:
			respondError(c, http.StatusInternalServerError, "Failed to delete profile", err)
		}
		return
	}

	logger.Info("Profile deleted via session",
		zap.String("mentor_id", session.MentorID))

	c.JSON(http.StatusOK, models.DeleteProfileResponse{Success: true})
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
		switch {
		// A save that raced a deletion (D70) wrote NOTHING — neither the row nor
		// the tags — so it must not answer 500 as if the outcome were unknown.
		case errors.Is(err, services.ErrProfileAlreadyDeleted):
			respondError(c, http.StatusConflict, "This profile has already been deleted", err)
		// Unknown tags are the client's to fix (a stale form after a taxonomy
		// change), not a server fault.
		case errors.Is(err, apperrors.ErrInvalidInput):
			respondError(c, http.StatusBadRequest, "Invalid profile data", err)
		default:
			respondError(c, http.StatusInternalServerError, "Failed to update profile", err)
		}
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

	imageURL, err := h.profileService.UploadPictureByMentorId(
		c.Request.Context(),
		session.MentorID,
		&req,
	)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrUploadsUnavailable):
			respondError(c, http.StatusServiceUnavailable, "Profile picture uploads are temporarily unavailable", err)
		case errors.Is(err, apperrors.ErrInvalidInput):
			// The image itself was rejected (unsupported type, too many
			// pixels, ...) — the mentor can act on that, so say what it was.
			respondError(c, http.StatusBadRequest, err.Error(), err)
		default:
			respondError(c, http.StatusInternalServerError, "Failed to upload picture", err)
		}
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
