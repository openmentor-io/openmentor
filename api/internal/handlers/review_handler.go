package handlers

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/internal/services"
)

// ReviewHandler handles review-related HTTP requests
type ReviewHandler struct {
	service services.ReviewServiceInterface
}

// NewReviewHandler creates a new review handler
func NewReviewHandler(service services.ReviewServiceInterface) *ReviewHandler {
	return &ReviewHandler{service: service}
}

// CheckReview handles GET /api/v1/reviews/:requestId/check
func (h *ReviewHandler) CheckReview(c *gin.Context) {
	requestID := c.Param("requestId")
	if requestID == "" {
		respondError(c, http.StatusBadRequest, "Missing request ID", fmt.Errorf("missing route param: requestId"))
		return
	}

	resp, err := h.service.CheckReview(c.Request.Context(), requestID)
	if err != nil {
		if errors.Is(err, services.ErrReviewRequestNotFound) {
			attachError(c, err)
			c.JSON(http.StatusNotFound, resp)
			return
		}
		respondError(c, http.StatusInternalServerError, "Failed to check review eligibility", err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// SubmitReview handles POST /api/v1/reviews/:requestId
func (h *ReviewHandler) SubmitReview(c *gin.Context) {
	requestID := c.Param("requestId")
	if requestID == "" {
		respondError(c, http.StatusBadRequest, "Missing request ID", fmt.Errorf("missing route param: requestId"))
		return
	}

	var req models.SubmitReviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		validationErrors := ParseValidationErrors(err)
		respondErrorWithDetails(c, http.StatusBadRequest, "Validation failed", validationErrors, err)
		return
	}

	resp, err := h.service.SubmitReview(c.Request.Context(), requestID, &req)
	if err != nil {
		if resp != nil && resp.Error != "" {
			attachError(c, err)
			if errors.Is(err, services.ErrReviewRequestNotFound) {
				c.JSON(http.StatusNotFound, resp)
				return
			}
			if errors.Is(err, services.ErrReviewAlreadyExists) || errors.Is(err, services.ErrReviewRequestNotDone) {
				c.JSON(http.StatusConflict, resp)
				return
			}
			c.JSON(http.StatusBadRequest, resp)
			return
		}
		respondError(c, http.StatusInternalServerError, "Internal server error", err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
