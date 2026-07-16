package handlers

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/internal/services"
)

// MentorConfirmationHandler handles the public email-confirmation endpoints
// of the draft-status registration workflow.
type MentorConfirmationHandler struct {
	service services.MentorConfirmationServiceInterface
}

// NewMentorConfirmationHandler creates a new MentorConfirmationHandler.
func NewMentorConfirmationHandler(service services.MentorConfirmationServiceInterface) *MentorConfirmationHandler {
	return &MentorConfirmationHandler{service: service}
}

// Confirm handles POST /api/v1/mentors/confirm
// Body: {token}. Responses:
//   - 200 {success:true} — confirmed, profile now in review
//   - 200 {success:true, already:true} — token already used / profile past draft
//   - 400 {success:false, code:"invalid_token"} — dead link
//   - 410 {success:false, code:"token_expired"} — client should offer resend
func (h *MentorConfirmationHandler) Confirm(c *gin.Context) {
	h.handle(c, h.service.ConfirmEmail)
}

// Resend handles POST /api/v1/mentors/confirm/resend
// Body: {token} (expired tokens accepted — the token identifies the
// mentor). Issues a fresh token and re-sends the confirmation email.
func (h *MentorConfirmationHandler) Resend(c *gin.Context) {
	h.handle(c, h.service.ResendConfirmation)
}

func (h *MentorConfirmationHandler) handle(
	c *gin.Context,
	action func(ctx context.Context, token string) (already bool, err error),
) {

	var req models.ConfirmMentorEmailRequest
	if bindErr := c.ShouldBindJSON(&req); bindErr != nil {
		c.JSON(http.StatusBadRequest, models.ConfirmMentorEmailResponse{
			Success: false,
			Error:   "A confirmation token is required",
			Code:    models.ConfirmationCodeInvalid,
		})
		return
	}

	already, err := action(c.Request.Context(), req.Token)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrConfirmationTokenInvalid):
			c.JSON(http.StatusBadRequest, models.ConfirmMentorEmailResponse{
				Success: false,
				Error:   "Invalid confirmation link",
				Code:    models.ConfirmationCodeInvalid,
			})
		case errors.Is(err, services.ErrConfirmationTokenExpired):
			c.JSON(http.StatusGone, models.ConfirmMentorEmailResponse{
				Success: false,
				Error:   "This confirmation link has expired",
				Code:    models.ConfirmationCodeExpired,
			})
		default:
			respondError(c, http.StatusInternalServerError, "Failed to process confirmation", err)
		}
		return
	}

	c.JSON(http.StatusOK, models.ConfirmMentorEmailResponse{Success: true, Already: already})
}
