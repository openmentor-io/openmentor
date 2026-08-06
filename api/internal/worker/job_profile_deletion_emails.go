package worker

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/openmentor-io/openmentor/api/pkg/analytics"
	"github.com/openmentor-io/openmentor/api/pkg/email"
	"github.com/openmentor-io/openmentor/api/pkg/logger"
)

// ProfileDeleted confirms to a mentor that their profile has been deleted, and
// ProfileRestored that it is back (D70). Both fire for a mentor-initiated and an
// admin-initiated action alike: the mentor is entitled to know what happened to
// their profile, and who pressed the button is not a difference the recipient
// cares about.
//
// Endpoints:
//
//	POST /jobs/profile-deleted?mentorId=<uuid>
//	POST /jobs/profile-restored?mentorId=<uuid>
//
// The deletion notice is the reason GetJobMentorByIDIncludingDeleted exists: the
// ordinary mentor lookup hides deleted profiles precisely so the worker stops
// emailing them, and this one email is about the deletion itself.
func (h *Handlers) ProfileDeleted(c *gin.Context) {
	h.profileDeletionNotice(c, profileDeletionNotice{
		job:          "profile-deleted",
		event:        analytics.EventMentorProfileDeleted,
		logPrefix:    "[Profile Deleted]",
		wantDeleted:  true,
		templateName: "profile-deleted",
		// Deliberately NO retention window in the props. The email asks the
		// mentor to reply "as soon as possible" instead of naming a deadline:
		// the retention period is an internal operational detail, and quoting
		// it turns a request for a prompt reply into a promise about a date we
		// would then be held to.
		props: func(_ *Handlers, mentor *JobMentor) map[string]interface{} {
			return map[string]interface{}{"first_name": mentor.Name}
		},
	})
}

func (h *Handlers) ProfileRestored(c *gin.Context) {
	h.profileDeletionNotice(c, profileDeletionNotice{
		job:          "profile-restored",
		event:        analytics.EventMentorProfileRestored,
		logPrefix:    "[Profile Restored]",
		wantDeleted:  false,
		templateName: "profile-restored",
		props: func(h *Handlers, mentor *JobMentor) map[string]interface{} {
			return map[string]interface{}{
				"first_name": mentor.Name,
				"login_url":  h.baseURL + "/mentor/login",
			}
		},
	})
}

// outcomeNotified is the one outcome that means the mentor actually received
// the notice. Named because it is both tracked and tested against, and a typo
// in either place would silently report every send as a failure.
const outcomeNotified = "success"

// profileDeletionNotice is the shape both notices share. They differ only in
// which side of deleted_at they expect to find the row on, and in the template.
type profileDeletionNotice struct {
	job       string
	event     string
	logPrefix string
	// wantDeleted is the replay guard: the ROW decides, not the trigger. A
	// delete notice redelivered after an admin restored the profile would
	// otherwise tell a mentor with a live profile that it was deleted, and a
	// restore notice redelivered after a second deletion would do the reverse.
	// Same reasoning as MentorModerationAction's status check.
	wantDeleted  bool
	templateName string
	props        func(*Handlers, *JobMentor) map[string]interface{}
}

func (h *Handlers) profileDeletionNotice(c *gin.Context, notice profileDeletionNotice) {
	ctx := c.Request.Context()

	trackOutcome := func(mentorID, outcome string) {
		h.track(ctx, notice.event, analytics.MentorDistinctID(mentorID), map[string]interface{}{
			"mentor_id": mentorID,
			"notified":  outcome == outcomeNotified,
			"outcome":   outcome,
		})
	}

	mentorID := c.Query("mentorId")
	if mentorID == "" {
		trackOutcome("unknown", "missing_mentor_id")
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "mentorId is required"})
		return
	}

	mentor, err := h.repo.GetJobMentorByIDIncludingDeleted(ctx, mentorID)
	if err != nil {
		logger.Error(notice.logPrefix+" Failed to fetch mentor",
			zap.String("mentor_id", mentorID), logger.RedactedError(err))
		trackOutcome(mentorID, "error")
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "error": "failed to fetch mentor"})
		return
	}
	if mentor == nil {
		// Purged, most likely: the retention window closed before this trigger
		// was retried. Nothing to notify and nothing to fix.
		logger.Warn(notice.logPrefix+" Mentor not found", zap.String("mentor_id", mentorID))
		trackOutcome(mentorID, "mentor_not_found")
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "mentor not found"})
		return
	}

	if isDeleted := mentor.DeletedAt != nil; isDeleted != notice.wantDeleted {
		logger.Warn(notice.logPrefix+" Profile deletion state no longer matches this notice; skipping",
			zap.String("mentor_id", mentorID),
			zap.Bool("currently_deleted", isDeleted),
			zap.Bool("expected_deleted", notice.wantDeleted),
		)
		// 200: a superseded callback is a no-op and a retry would find the same
		// state, so answering an error would only invite the redelivery that
		// caused this.
		trackOutcome(mentorID, "superseded")
		c.JSON(http.StatusOK, gin.H{"success": true, "superseded": true})
		return
	}

	if mentor.Email == "" {
		logger.Warn(notice.logPrefix+" Mentor has no email", zap.String("mentor_id", mentorID))
		trackOutcome(mentorID, "missing_email")
		c.JSON(http.StatusUnprocessableEntity, gin.H{"success": false, "error": "mentor has no email"})
		return
	}

	message := email.Message{
		TemplateName: notice.templateName,
		Recipient:    mentor.Email,
		Props:        notice.props(h, mentor),
	}
	if sendErr := h.sendEmail(ctx, notice.job, message); sendErr != nil {
		trackOutcome(mentorID, "error")
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "error": "failed to send email"})
		return
	}

	logger.Info(notice.logPrefix+" Notification sent", zap.String("mentor_id", mentorID))
	trackOutcome(mentorID, outcomeNotified)
	c.JSON(http.StatusOK, gin.H{"success": true})
}
