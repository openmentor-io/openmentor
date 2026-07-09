package worker

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openmentor-io/openmentor/api/pkg/analytics"
)

func TestNewMentorWatcherHappyPath(t *testing.T) {
	env := newJobsTestEnv()
	mentor := testMentor("m1")
	mentor.Name = "John  Doe "             // trailing space + double space
	mentor.PreferredContact = " @johndoe " // free text, only whitespace is trimmed
	env.repo.mentors["m1"] = mentor

	w := env.do(http.MethodPost, "/jobs/new-mentor-watcher?mentorId=m1", nil)

	assert.Equal(t, http.StatusOK, w.Code)

	// DB write: trimmed fields, pending status, login token + ~100 day expiry.
	require.Len(t, env.repo.finalized, 1)
	update := env.repo.finalized[0]
	assert.Equal(t, "m1", update.MentorID)
	assert.Equal(t, "John Doe", update.Name)
	assert.Equal(t, "@johndoe", update.PreferredContact)
	assert.Equal(t, "pending", update.Status)
	assert.Equal(t, "john-doe-42", update.Slug, "existing slug must be kept, not regenerated")
	assert.NotEmpty(t, update.LoginToken)
	assert.WithinDuration(t, time.Now().AddDate(0, 0, 100), update.LoginTokenExpiresAt, time.Minute)
	assert.GreaterOrEqual(t, update.SortOrder, 0)
	assert.Less(t, update.SortOrder, 1000)

	// Emails: moderator notification + mentor welcome.
	require.Equal(t, []string{"new-mentor-moderator", "new-mentor"}, env.sender.templates())
	moderatorMsg := env.sender.attempts[0]
	assert.Equal(t, testModeratorsEmail, moderatorMsg.Recipient)
	assert.Equal(t, "John Doe", moderatorMsg.Props["mentor_name"])
	assert.Equal(t, "john@example.com", moderatorMsg.Props["mentor_email"])
	assert.Equal(t, "Engineer @ Acme", moderatorMsg.Props["mentor_job"])
	mentorMsg := env.sender.attempts[1]
	assert.Equal(t, "john@example.com", mentorMsg.Recipient)
	assert.Equal(t, "John Doe", mentorMsg.Props["first_name"])

	// Analytics: success event with duplicates_count.
	event := env.tracker.last()
	require.NotNil(t, event)
	assert.Equal(t, analytics.EventNewMentorWatcherProcessed, event.event)
	assert.Equal(t, "mentor:m1", event.distinctID)
	assert.Equal(t, "success", event.props["outcome"])
	assert.Equal(t, "pending", event.props["status"])
	assert.Equal(t, 0, event.props["duplicates_count"])
}

func TestNewMentorWatcherDuplicateEmailDeclines(t *testing.T) {
	env := newJobsTestEnv()
	env.repo.mentors["m1"] = testMentor("m1")
	env.repo.duplicates = 1

	w := env.do(http.MethodPost, "/jobs/new-mentor-watcher?mentorId=m1", nil)

	assert.Equal(t, http.StatusOK, w.Code)

	require.Len(t, env.repo.finalized, 1)
	assert.Equal(t, "declined", env.repo.finalized[0].Status)

	require.Equal(t, []string{"new-mentor-duplicate"}, env.sender.templates())
	assert.Equal(t, "john@example.com", env.sender.attempts[0].Recipient)

	event := env.tracker.last()
	require.NotNil(t, event)
	assert.Equal(t, "success", event.props["outcome"])
	assert.Equal(t, "declined", event.props["status"])
	assert.Equal(t, 1, event.props["duplicates_count"])
}

func TestNewMentorWatcherGeneratesSlugWhenMissing(t *testing.T) {
	env := newJobsTestEnv()
	mentor := testMentor("m1")
	mentor.Slug = ""
	env.repo.mentors["m1"] = mentor

	w := env.do(http.MethodGet, "/jobs/new-mentor-watcher?mentorId=m1", nil)

	assert.Equal(t, http.StatusOK, w.Code)
	require.Len(t, env.repo.finalized, 1)
	assert.Equal(t, "john-doe-42", env.repo.finalized[0].Slug)
}

func TestNewMentorWatcherMissingRecord(t *testing.T) {
	env := newJobsTestEnv()

	tests := []struct {
		name        string
		path        string
		wantOutcome string
	}{
		{"missing mentorId param", "/jobs/new-mentor-watcher", "missing_mentor_id"},
		{"mentor not found", "/jobs/new-mentor-watcher?mentorId=ghost", "mentor_not_found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := env.do(http.MethodPost, tt.path, nil)

			assert.Equal(t, http.StatusNotFound, w.Code)
			assert.Empty(t, env.sender.attempts, "no email must be sent")
			assert.Empty(t, env.repo.finalized, "no DB write must happen")

			event := env.tracker.last()
			require.NotNil(t, event)
			assert.Equal(t, analytics.EventNewMentorWatcherProcessed, event.event)
			assert.Equal(t, tt.wantOutcome, event.props["outcome"])
		})
	}
}

func TestNewMentorWatcherEmailFailureResilience(t *testing.T) {
	env := newJobsTestEnv()
	env.repo.mentors["m1"] = testMentor("m1")
	env.sender.failTemplates["new-mentor-moderator"] = true

	w := env.do(http.MethodPost, "/jobs/new-mentor-watcher?mentorId=m1", nil)

	// One failed send must not skip the other, and the DB write stays.
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Equal(t, []string{"new-mentor-moderator", "new-mentor"}, env.sender.templates())
	assert.Len(t, env.repo.finalized, 1)

	event := env.tracker.last()
	require.NotNil(t, event)
	assert.Equal(t, "error", event.props["outcome"])
	assert.Equal(t, "email_send_failed", event.props["error_type"])
}
