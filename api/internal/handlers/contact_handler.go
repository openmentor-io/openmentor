package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/internal/services"
)

type ContactHandler struct {
	service services.ContactServiceInterface
}

func NewContactHandler(service services.ContactServiceInterface) *ContactHandler {
	return &ContactHandler{service: service}
}

func (h *ContactHandler) ContactMentor(c *gin.Context) {
	var req models.ContactMentorRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		validationErrors := ParseValidationErrors(err)
		respondErrorWithDetails(c, http.StatusBadRequest, "Validation failed", validationErrors, err)
		return
	}

	resp, err := h.service.SubmitContactForm(c.Request.Context(), &req)
	if err != nil {
		// The mentor not accepting requests is a state conflict, not bad input:
		// the form was valid, and nothing about it can be edited to make the
		// request land. 409 keeps it apart from validation failures in the logs.
		if errors.Is(err, services.ErrMentorNotContactable) {
			attachError(c, err)
			c.JSON(http.StatusConflict, resp)
			return
		}
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
