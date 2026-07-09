package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/openmentor-io/openmentor-api/internal/models"
	"github.com/openmentor-io/openmentor-api/internal/services"
)

// RegistrationHandler handles mentor registration endpoints
type RegistrationHandler struct {
	service services.RegistrationServiceInterface
}

// NewRegistrationHandler creates a new registration handler
func NewRegistrationHandler(service services.RegistrationServiceInterface) *RegistrationHandler {
	return &RegistrationHandler{service: service}
}

// RegisterMentor handles POST /api/v1/register-mentor
func (h *RegistrationHandler) RegisterMentor(c *gin.Context) {
	var req models.RegisterMentorRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		validationErrors := ParseValidationErrors(err)
		respondErrorWithDetails(c, http.StatusBadRequest, "Validation failed", validationErrors, err)
		return
	}

	resp, err := h.service.RegisterMentor(c.Request.Context(), &req)
	if err != nil {
		if resp != nil && resp.Error != "" {
			attachError(c, err)
			c.JSON(http.StatusBadRequest, resp)
			return
		}
		respondError(c, http.StatusInternalServerError, "Internal server error", err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
