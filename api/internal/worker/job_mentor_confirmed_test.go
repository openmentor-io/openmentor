package worker

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openmentor-io/openmentor/api/pkg/analytics"
)

func TestMentorConfirmedHappyPath(t *testing.T) {
	env := newJobsTestEnv()
	mentor := testMentor("m1")
	mentor.Status = "pending"
	env.repo.mentors["m1"] = mentor

	w := env.do(http.MethodPost, "/jobs/mentor-confirmed?mentorId=m1", nil)

	assert.Equal(t, http.StatusOK, w.Code)

	// Emails: moderator notification + mentor "in review" welcome (the
	// pair the pre-draft-workflow new-mentor-watcher used to send).
	require.Equal(t, []string{"new-mentor-moderator", "new-mentor"}, env.sender.templates())
	moderatorMsg := env.sender.attempts[0]
	assert.Equal(t, testModeratorsEmail, moderatorMsg.Recipient)
	assert.Equal(t, "John Doe", moderatorMsg.Props["mentor_name"])
	assert.Equal(t, "john@example.com", moderatorMsg.Props["mentor_email"])
	assert.Equal(t, "Engineer @ Acme", moderatorMsg.Props["mentor_job"])
	mentorMsg := env.sender.attempts[1]
	assert.Equal(t, "john@example.com", mentorMsg.Recipient)
	assert.Equal(t, "John Doe", mentorMsg.Props["first_name"])

	// No status writes: the API already moved draft -> pending.
	assert.Empty(t, env.repo.statusUpdates)
	assert.Empty(t, env.repo.finalized)

	event := env.tracker.last()
	require.NotNil(t, event)
	assert.Equal(t, analytics.EventMentorConfirmedProcessed, event.event)
	assert.Equal(t, "mentor:m1", event.distinctID)
	assert.Equal(t, "success", event.props["outcome"])
}

func TestMentorConfirmedMissingRecord(t *testing.T) {
	env := newJobsTestEnv()

	tests := []struct {
		name        string
		path        string
		wantOutcome string
	}{
		{"missing mentorId param", "/jobs/mentor-confirmed", "missing_mentor_id"},
		{"mentor not found", "/jobs/mentor-confirmed?mentorId=ghost", "mentor_not_found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := env.do(http.MethodPost, tt.path, nil)

			assert.Equal(t, http.StatusNotFound, w.Code)
			assert.Empty(t, env.sender.attempts, "no email must be sent")

			event := env.tracker.last()
			require.NotNil(t, event)
			assert.Equal(t, analytics.EventMentorConfirmedProcessed, event.event)
			assert.Equal(t, tt.wantOutcome, event.props["outcome"])
		})
	}
}

func TestMentorConfirmedEmailFailureResilience(t *testing.T) {
	env := newJobsTestEnv()
	env.repo.mentors["m1"] = testMentor("m1")
	env.sender.failTemplates["new-mentor-moderator"] = true

	w := env.do(http.MethodPost, "/jobs/mentor-confirmed?mentorId=m1", nil)

	// One failed send must not skip the other.
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Equal(t, []string{"new-mentor-moderator", "new-mentor"}, env.sender.templates())

	event := env.tracker.last()
	require.NotNil(t, event)
	assert.Equal(t, "error", event.props["outcome"])
	assert.Equal(t, "email_send_failed", event.props["error_type"])
}

func TestMentorConfirmEmailHappyPath(t *testing.T) {
	env := newJobsTestEnv()
	mentor := testMentor("m1")
	mentor.Status = "draft"
	mentor.EmailConfirmationToken = "mcf_fresh_token"
	env.repo.mentors["m1"] = mentor

	w := env.do(http.MethodPost, "/jobs/mentor-confirm-email?mentorId=m1", nil)

	assert.Equal(t, http.StatusOK, w.Code)

	require.Equal(t, []string{"mentor-confirm-email"}, env.sender.templates())
	msg := env.sender.attempts[0]
	assert.Equal(t, "john@example.com", msg.Recipient)
	assert.Equal(t, "John Doe", msg.Props["first_name"])
	assert.Equal(t, "https://openmentor.io/mentor/confirm?token=mcf_fresh_token", msg.Props["confirm_url"])

	event := env.tracker.last()
	require.NotNil(t, event)
	assert.Equal(t, analytics.EventMentorConfirmEmailSent, event.event)
	assert.Equal(t, "success", event.props["outcome"])
}

func TestMentorConfirmEmailWithoutToken(t *testing.T) {
	env := newJobsTestEnv()
	env.repo.mentors["m1"] = testMentor("m1") // no confirmation token

	w := env.do(http.MethodPost, "/jobs/mentor-confirm-email?mentorId=m1", nil)

	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Empty(t, env.sender.attempts, "no email must be sent without a token")

	event := env.tracker.last()
	require.NotNil(t, event)
	assert.Equal(t, "no_confirmation_token", event.props["outcome"])
}

func TestMentorConfirmEmailMissingRecord(t *testing.T) {
	env := newJobsTestEnv()

	w := env.do(http.MethodPost, "/jobs/mentor-confirm-email?mentorId=ghost", nil)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Empty(t, env.sender.attempts)
}
