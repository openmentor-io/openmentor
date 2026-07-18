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

	// DB write: trimmed fields, draft status, single-use email confirmation
	// token + 24h expiry. SECURITY: no usable login token is pre-provisioned
	// (L2) — FinalizeNewMentor NULLs login_token; a token is only ever minted
	// on demand by RequestLogin.
	require.Len(t, env.repo.finalized, 1)
	update := env.repo.finalized[0]
	assert.Equal(t, "m1", update.MentorID)
	assert.Equal(t, "John Doe", update.Name)
	assert.Equal(t, "@johndoe", update.PreferredContact)
	assert.Equal(t, "draft", update.Status, "mentor stays draft until the email is confirmed")
	assert.Equal(t, "john-doe-42", update.Slug, "existing slug must be kept, not regenerated")
	assert.GreaterOrEqual(t, update.SortOrder, 0)
	assert.Less(t, update.SortOrder, 1000)
	require.NotNil(t, update.EmailConfirmationToken)
	assert.NotEmpty(t, *update.EmailConfirmationToken)
	require.NotNil(t, update.EmailConfirmationExpiresAt)
	assert.WithinDuration(t, time.Now().Add(24*time.Hour), *update.EmailConfirmationExpiresAt, time.Minute)

	// Email: only the confirmation request — the mentor welcome and the
	// moderator notification move to the mentor-confirmed job.
	require.Equal(t, []string{"mentor-confirm-email"}, env.sender.templates())
	confirmMsg := env.sender.attempts[0]
	assert.Equal(t, "john@example.com", confirmMsg.Recipient)
	assert.Equal(t, "John Doe", confirmMsg.Props["first_name"])
	assert.Equal(t,
		"https://openmentor.io/mentor/confirm?token="+*update.EmailConfirmationToken,
		confirmMsg.Props["confirm_url"])

	// Analytics: success event with duplicates_count.
	event := env.tracker.last()
	require.NotNil(t, event)
	assert.Equal(t, analytics.EventNewMentorWatcherProcessed, event.event)
	assert.Equal(t, "mentor:m1", event.distinctID)
	assert.Equal(t, "success", event.props["outcome"])
	assert.Equal(t, "draft", event.props["status"])
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
	assert.Nil(t, env.repo.finalized[0].EmailConfirmationToken, "declined duplicates get no confirmation token")

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
	env.sender.failTemplates["mentor-confirm-email"] = true

	w := env.do(http.MethodPost, "/jobs/new-mentor-watcher?mentorId=m1", nil)

	// A failed send reports an error, but the DB write stays.
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Equal(t, []string{"mentor-confirm-email"}, env.sender.templates())
	assert.Len(t, env.repo.finalized, 1)

	event := env.tracker.last()
	require.NotNil(t, event)
	assert.Equal(t, "error", event.props["outcome"])
	assert.Equal(t, "email_send_failed", event.props["error_type"])
}
