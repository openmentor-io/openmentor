package worker

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/openmentor-io/openmentor-api/pkg/analytics"
	"github.com/openmentor-io/openmentor-api/pkg/email"
	"github.com/openmentor-io/openmentor-api/pkg/logger"
)

// reviewPreviewLength mirrors the 500-char truncation in the func's
// ReviewNotificationEmailMessage.
const reviewPreviewLength = 500

// ProcessMenteeReview ports openmentor-func/process-mentee-review/index.ts:
// notify the mentor that a mentee left a review.
//
// Behavioral delta vs the func: when the mentor row is missing the func
// threw (tracking mentor_not_found AND a second "error" event, then
// answering 503); the worker answers a meaningful 404 with a single
// mentor_not_found event.
func (h *Handlers) ProcessMenteeReview(c *gin.Context) {
	ctx := c.Request.Context()
	const job = "process-mentee-review"

	reviewID := c.Query("reviewId")
	if reviewID == "" {
		h.track(ctx, analytics.EventReviewSubmitted, analytics.SystemDistinctID("worker"), map[string]interface{}{
			"outcome": "missing_review_id",
		})
		c.JSON(http.StatusNotFound, gin.H{"error": "reviewId is required"})
		return
	}

	review, err := h.repo.GetJobReviewByID(ctx, reviewID)
	if err != nil {
		logger.Error("[Mentor Review] Failed to fetch review", zap.String("review_id", reviewID), zap.Error(err))
		h.track(ctx, analytics.EventReviewSubmitted, analytics.ReviewDistinctID(reviewID), map[string]interface{}{
			"review_id":  reviewID,
			"outcome":    "error",
			"error_type": "db_error",
		})
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "failed to fetch review"})
		return
	}
	if review == nil {
		logger.Warn("[Mentor Review] Review not found", zap.String("review_id", reviewID))
		h.track(ctx, analytics.EventReviewSubmitted, analytics.ReviewDistinctID(reviewID), map[string]interface{}{
			"review_id": reviewID,
			"outcome":   "review_not_found",
		})
		c.JSON(http.StatusNotFound, gin.H{"error": "review not found"})
		return
	}

	mentor, err := h.repo.GetJobMentorByID(ctx, review.MentorID)
	if err != nil {
		logger.Error("[Mentor Review] Failed to fetch mentor",
			zap.String("review_id", reviewID), zap.String("mentor_id", review.MentorID), zap.Error(err))
		h.track(ctx, analytics.EventReviewSubmitted, analytics.ReviewDistinctID(reviewID), map[string]interface{}{
			"review_id":  reviewID,
			"outcome":    "error",
			"error_type": "db_error",
		})
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "failed to fetch mentor"})
		return
	}
	if mentor == nil {
		logger.Warn("[Mentor Review] Mentor not found",
			zap.String("review_id", reviewID), zap.String("mentor_id", review.MentorID))
		h.track(ctx, analytics.EventReviewSubmitted, analytics.RequestDistinctID(review.RequestID), map[string]interface{}{
			"review_id":  review.ID,
			"request_id": review.RequestID,
			"mentor_id":  review.MentorID,
			"outcome":    "mentor_not_found",
		})
		c.JSON(http.StatusNotFound, gin.H{"error": "mentor not found"})
		return
	}

	sendErr := h.sendEmail(ctx, job, email.Message{
		TemplateName: "new-review",
		Recipient:    mentor.Email,
		Props: map[string]interface{}{
			"first_name":  mentor.Name,
			"mentee_name": review.MenteeName,
			"review_text": truncateReview(review.ReviewText),
		},
	})
	if sendErr != nil {
		h.track(ctx, analytics.EventReviewSubmitted, analytics.ReviewDistinctID(reviewID), map[string]interface{}{
			"review_id":  reviewID,
			"outcome":    "error",
			"error_type": "email_send_failed",
		})
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "failed to send email"})
		return
	}

	logger.Info("[Mentor Review] Sent notification",
		zap.String("review_id", reviewID), zap.String("mentor_id", mentor.ID))
	h.track(ctx, analytics.EventReviewSubmitted, analytics.RequestDistinctID(review.RequestID), map[string]interface{}{
		"review_id":  review.ID,
		"request_id": review.RequestID,
		"mentor_id":  mentor.ID,
		"outcome":    "success",
	})
	c.JSON(http.StatusOK, gin.H{"success": true, "reviewId": review.ID})
}

// truncateReview trims the review text for the email preview, mirroring the
// func's `review.substring(0, 500) + '...'` (rune-safe in Go).
func truncateReview(text string) string {
	runes := []rune(text)
	if len(runes) <= reviewPreviewLength {
		return text
	}
	return string(runes[:reviewPreviewLength]) + "..."
}
