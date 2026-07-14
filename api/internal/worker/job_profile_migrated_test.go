package worker

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openmentor-io/openmentor/api/pkg/analytics"
)

func TestProfileMigratedSendsEmail(t *testing.T) {
	env := newJobsTestEnv()
	mentor := testMentor("m1")
	mentor.Status = "inactive" // migrated mentors land approved-but-hidden
	env.repo.mentors["m1"] = mentor

	w := env.do(http.MethodPost, "/jobs/profile-migrated?mentorId=m1", nil)

	assert.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, []string{"profile-migrated"}, env.sender.templates())
	msg := env.sender.attempts[0]
	assert.Equal(t, "john@example.com", msg.Recipient)
	assert.Equal(t, "John Doe", msg.Props["first_name"])
	assert.Equal(t, "https://openmentor.io/mentor/john-doe-42", msg.Props["mentor_profile_url"])

	event := env.tracker.last()
	require.NotNil(t, event)
	assert.Equal(t, analytics.EventMentorProfileMigrated, event.event)
	assert.Equal(t, "mentor:m1", event.distinctID)
	assert.Equal(t, "success", event.props["outcome"])
}

func TestProfileMigratedMissingMentorID(t *testing.T) {
	env := newJobsTestEnv()

	w := env.do(http.MethodPost, "/jobs/profile-migrated", nil)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Empty(t, env.sender.templates())
}

func TestProfileMigratedMentorNotFound(t *testing.T) {
	env := newJobsTestEnv()

	w := env.do(http.MethodPost, "/jobs/profile-migrated?mentorId=absent", nil)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Empty(t, env.sender.templates())
}

func TestProfileMigratedMentorWithoutEmail(t *testing.T) {
	env := newJobsTestEnv()
	mentor := testMentor("m1")
	mentor.Email = ""
	env.repo.mentors["m1"] = mentor

	w := env.do(http.MethodPost, "/jobs/profile-migrated?mentorId=m1", nil)

	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
	assert.Empty(t, env.sender.templates())

	event := env.tracker.last()
	require.NotNil(t, event)
	assert.Equal(t, "missing_email", event.props["outcome"])
}

func TestProfileMigratedEmailFailure(t *testing.T) {
	env := newJobsTestEnv()
	env.repo.mentors["m1"] = testMentor("m1")
	env.sender.failAll = true

	w := env.do(http.MethodPost, "/jobs/profile-migrated?mentorId=m1", nil)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	event := env.tracker.last()
	require.NotNil(t, event)
	assert.Equal(t, "error", event.props["outcome"])
}
