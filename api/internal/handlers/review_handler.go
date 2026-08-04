package handlers

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/internal/services"
)

// reviewLinkGoneMessage mirrors the service's copy for a dead capability. It is
// duplicated here for the one case that never reaches the service: an
// unparseable request body.
const reviewLinkGoneMessage = "This review link has already been used or has expired."

// ReviewHandler handles review-related HTTP requests
type ReviewHandler struct {
	service services.ReviewServiceInterface
}

// NewReviewHandler creates a new review handler
func NewReviewHandler(service services.ReviewServiceInterface) *ReviewHandler {
	return &ReviewHandler{service: service}
}

// CheckReviewByToken handles POST /api/v1/reviews/check.
//
// A POST for what is logically a read, because the review capability travels in
// the BODY: a GET would put it in the query string, where it reaches the access
// log, the span's url.query and PostHog's $current_url (H4). Nothing in this
// handler copies the token into a logged field.
func (h *ReviewHandler) CheckReviewByToken(c *gin.Context) {
	var req models.CheckReviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// Deliberately NOT ParseValidationErrors: a malformed body and a dead
		// token get the same answer, so the shape of a valid token is not probeable.
		attachError(c, fmt.Errorf("malformed review check body"))
		c.JSON(http.StatusNotFound, models.ReviewCheckResponse{CanSubmit: false, Error: reviewLinkGoneMessage})
		return
	}

	resp, err := h.service.CheckReviewByToken(c.Request.Context(), req.Token)
	if err != nil {
		if errors.Is(err, services.ErrReviewLinkInvalid) {
			attachError(c, err)
			c.JSON(http.StatusNotFound, resp)
			return
		}
		respondError(c, http.StatusInternalServerError, "Failed to check review eligibility", err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// SubmitReviewWithToken handles POST /api/v1/reviews/submit — the token-bearing
// submission. The capability is in the body; the URL is a constant.
func (h *ReviewHandler) SubmitReviewWithToken(c *gin.Context) {
	var req models.SubmitReviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		validationErrors := ParseValidationErrors(err)
		respondErrorWithDetails(c, http.StatusBadRequest, "Validation failed", validationErrors, err)
		return
	}

	resp, err := h.service.SubmitReviewWithToken(c.Request.Context(), req.Token, &req)
	if err != nil {
		respondReviewSubmitError(c, resp, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// CheckReview handles GET /api/v1/reviews/:requestId/check — LEGACY, removed at
// the H4 cutover.
func (h *ReviewHandler) CheckReview(c *gin.Context) {
	requestID := c.Param("requestId")
	if requestID == "" {
		respondError(c, http.StatusBadRequest, "Missing request ID", fmt.Errorf("missing route param: requestId"))
		return
	}

	resp, err := h.service.CheckReview(c.Request.Context(), requestID)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrReviewLegacyPathDisabled):
			attachError(c, err)
			c.JSON(http.StatusGone, resp)
		case errors.Is(err, services.ErrReviewRequestNotFound):
			attachError(c, err)
			c.JSON(http.StatusNotFound, resp)
		default:
			respondError(c, http.StatusInternalServerError, "Failed to check review eligibility", err)
		}
		return
	}

	c.JSON(http.StatusOK, resp)
}

// SubmitReview handles POST /api/v1/reviews/:requestId — LEGACY, removed at the
// H4 cutover.
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
		respondReviewSubmitError(c, resp, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// respondReviewSubmitError maps the typed service errors onto status codes,
// shared by both submission paths so they cannot drift.
//
// A spent, expired or unknown capability is a 409, NOT a 404: the second of two
// submissions with the same token must be a typed conflict (H4 acceptance), and
// its message is identical to the already-reviewed case so neither reveals
// anything about the request behind the link.
func respondReviewSubmitError(c *gin.Context, resp *models.SubmitReviewResponse, err error) {
	if resp == nil || resp.Error == "" {
		respondError(c, http.StatusInternalServerError, "Internal server error", err)
		return
	}
	attachError(c, err)
	switch {
	case errors.Is(err, services.ErrReviewLegacyPathDisabled):
		c.JSON(http.StatusGone, resp)
	case errors.Is(err, services.ErrReviewLinkInvalid),
		errors.Is(err, services.ErrReviewAlreadyExists),
		errors.Is(err, services.ErrReviewRequestNotDone):
		c.JSON(http.StatusConflict, resp)
	case errors.Is(err, services.ErrReviewRequestNotFound):
		c.JSON(http.StatusNotFound, resp)
	default:
		c.JSON(http.StatusBadRequest, resp)
	}
}
