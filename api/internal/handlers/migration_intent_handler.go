package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/internal/services"
)

// MigrationIntentHandler handles getmentor.dev migration opt-ins from the
// public /migrate page.
type MigrationIntentHandler struct {
	service *services.MigrationIntentService
}

// NewMigrationIntentHandler creates a new migration intent handler.
func NewMigrationIntentHandler(service *services.MigrationIntentService) *MigrationIntentHandler {
	return &MigrationIntentHandler{service: service}
}

// ScheduleMigration handles POST /api/v1/migration/intents
func (h *MigrationIntentHandler) ScheduleMigration(c *gin.Context) {
	var req models.ScheduleMigrationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		validationErrors := ParseValidationErrors(err)
		respondErrorWithDetails(c, http.StatusBadRequest, "Validation failed", validationErrors, err)
		return
	}

	resp, err := h.service.ScheduleMigration(c.Request.Context(), &req)
	if err != nil {
		// The service returns a user-safe message on the response; captcha
		// and slug-shape failures are the client's fault, the rest is ours.
		status := http.StatusBadRequest
		if resp != nil && resp.Error == "Failed to schedule the migration" {
			status = http.StatusInternalServerError
		}
		attachError(c, err)
		c.JSON(status, resp)
		return
	}

	c.JSON(http.StatusOK, resp)
}
