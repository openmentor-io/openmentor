package worker

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/openmentor-io/openmentor/api/pkg/analytics"
	"github.com/openmentor-io/openmentor/api/pkg/email"
	"github.com/openmentor-io/openmentor/api/pkg/logger"
)

// ProfileMigrated notifies a mentor that their getmentor.dev profile has
// been migrated to OpenMentor. It is fired by the migration tooling
// (infra/migration/migrate-mentors.sh) after a successful import, not by
// the API. The migrated mentor is approved but inactive: the email tells
// them to log in, review the translated profile and flip visibility on.
//
// Endpoint: POST /jobs/profile-migrated?mentorId=<uuid>
func (h *Handlers) ProfileMigrated(c *gin.Context) {
	ctx := c.Request.Context()
	const job = "profile-migrated"

	trackOutcome := func(mentorID, outcome string) {
		h.track(ctx, analytics.EventMentorProfileMigrated, analytics.MentorDistinctID(mentorID), map[string]interface{}{
			"outcome": outcome,
		})
	}

	mentorID := c.Query("mentorId")
	if mentorID == "" {
		trackOutcome("unknown", "missing_mentor_id")
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "mentorId is required"})
		return
	}

	mentor, err := h.repo.GetJobMentorByID(ctx, mentorID)
	if err != nil {
		logger.Error("[Profile Migrated] Failed to fetch mentor", zap.String("mentor_id", mentorID), zap.Error(err))
		trackOutcome(mentorID, "error")
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "error": "failed to fetch mentor"})
		return
	}
	if mentor == nil {
		logger.Warn("[Profile Migrated] Mentor not found", zap.String("mentor_id", mentorID))
		trackOutcome(mentorID, "mentor_not_found")
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "mentor not found"})
		return
	}
	if mentor.Email == "" {
		logger.Warn("[Profile Migrated] Mentor has no email", zap.String("mentor_id", mentorID))
		trackOutcome(mentorID, "missing_email")
		c.JSON(http.StatusUnprocessableEntity, gin.H{"success": false, "error": "mentor has no email"})
		return
	}

	message := email.Message{
		TemplateName: "profile-migrated",
		Recipient:    mentor.Email,
		Props: map[string]interface{}{
			"first_name":         mentor.Name,
			"mentor_profile_url": h.mentorProfileURL(mentor.Slug),
		},
	}
	if sendErr := h.sendEmail(ctx, job, message); sendErr != nil {
		trackOutcome(mentorID, "error")
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "error": "failed to send email"})
		return
	}

	logger.Info("[Profile Migrated] Notification sent",
		zap.String("mentor_id", mentorID),
		zap.String("slug", mentor.Slug),
	)
	trackOutcome(mentorID, "success")
	c.JSON(http.StatusOK, gin.H{"success": true})
}
