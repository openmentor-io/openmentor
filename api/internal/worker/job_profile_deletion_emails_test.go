package worker

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openmentor-io/openmentor/api/config"
	"github.com/openmentor-io/openmentor/api/pkg/analytics"
	"github.com/openmentor-io/openmentor/api/pkg/email/templates"
)

// deletedTestMentor is a mentor whose profile carries the deletion marker. It
// goes in the fake's deletedMentors map, which only the …IncludingDeleted
// lookup can see — the same split the real repository enforces.
func deletedTestMentor(id string) *JobMentor {
	m := testMentor(id)
	deletedAt := time.Now().Add(-time.Minute)
	m.DeletedAt = &deletedAt
	m.Status = "inactive" // deletion sets inactive; deleted_at is the marker
	return m
}

func deletionEnv(retentionDays int) *jobsTestEnv {
	return newJobsTestEnvWithConfig(func(cfg *config.Config) {
		cfg.Worker.ProfilePurgeRetentionDays = retentionDays
	})
}

func TestProfileDeletedSendsConfirmationToTheDeletedMentor(t *testing.T) {
	env := deletionEnv(30)
	env.repo.deletedMentors["m1"] = deletedTestMentor("m1")

	w := env.do(http.MethodPost, "/jobs/profile-deleted?mentorId=m1", nil)

	assert.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, []string{"profile-deleted"}, env.sender.templates())
	msg := env.sender.attempts[0]
	assert.Equal(t, "john@example.com", msg.Recipient)
	assert.Equal(t, "John Doe", msg.Props["first_name"])

	event := env.tracker.last()
	require.NotNil(t, event)
	assert.Equal(t, analytics.EventMentorProfileDeleted, event.event)
	assert.Equal(t, "success", event.props["outcome"])
	assert.Equal(t, true, event.props["notified"])
}

// The retention window is internal. The email asks for a prompt reply instead
// of naming a deadline, so neither the props nor the rendered body may carry
// it — and the rendering is checked because a literal in the template asset
// would never show up in the props.
func TestProfileDeletedDoesNotDiscloseTheRetentionWindow(t *testing.T) {
	env := deletionEnv(7)
	env.repo.deletedMentors["m1"] = deletedTestMentor("m1")

	w := env.do(http.MethodPost, "/jobs/profile-deleted?mentorId=m1", nil)
	require.Equal(t, http.StatusOK, w.Code)

	msg := env.sender.attempts[0]
	assert.NotContains(t, msg.Props, "retention_days")

	rendered, err := templates.Render(msg.TemplateName, msg.Props)
	require.NoError(t, err)
	for _, part := range []string{rendered.Subject, rendered.HTML, rendered.Text} {
		assert.NotContains(t, part, "7 days")
		assert.NotContains(t, part, "30 days")
		assert.NotRegexp(t, `(?i)\b(kept|retained|erased|deleted)\s+(for|after|within)\s+\d+`, part)
	}
	assert.Contains(t, rendered.Text, "as soon as possible")
}

// The ordinary mentor lookup hides deleted profiles precisely so the worker
// stops emailing them. This job is the one exception, and it has to actually
// take it — a regression to GetJobMentorByID would make the confirmation
// unsendable, silently.
func TestProfileDeletedReachesAProfileTheOrdinaryLookupCannotSee(t *testing.T) {
	env := deletionEnv(30)
	env.repo.deletedMentors["m1"] = deletedTestMentor("m1")

	// Precondition: invisible to the ordinary lookup.
	live, err := env.repo.GetJobMentorByID(t.Context(), "m1")
	require.NoError(t, err)
	require.Nil(t, live, "a deleted profile must not be visible to GetJobMentorByID")

	w := env.do(http.MethodPost, "/jobs/profile-deleted?mentorId=m1", nil)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, []string{"profile-deleted"}, env.sender.templates())
}

func TestProfileRestoredSendsConfirmation(t *testing.T) {
	env := deletionEnv(30)
	env.repo.mentors["m1"] = testMentor("m1") // restored: no deletion marker

	w := env.do(http.MethodPost, "/jobs/profile-restored?mentorId=m1", nil)

	assert.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, []string{"profile-restored"}, env.sender.templates())
	msg := env.sender.attempts[0]
	assert.Equal(t, "john@example.com", msg.Recipient)
	assert.Equal(t, "John Doe", msg.Props["first_name"])
	assert.Equal(t, "https://openmentor.io/mentor/login", msg.Props["login_url"])

	event := env.tracker.last()
	require.NotNil(t, event)
	assert.Equal(t, analytics.EventMentorProfileRestored, event.event)
	assert.Equal(t, "success", event.props["outcome"])
}

// The ROW decides, not the trigger — same guard as MentorModerationAction.
func TestProfileDeletionNoticesAreSupersededByTheCurrentState(t *testing.T) {
	t.Run("delete notice redelivered after a restore", func(t *testing.T) {
		env := deletionEnv(30)
		env.repo.mentors["m1"] = testMentor("m1") // restored in the meantime

		w := env.do(http.MethodPost, "/jobs/profile-deleted?mentorId=m1", nil)

		// 200 so the caller stops retrying, but no email: telling a mentor with
		// a live profile that it was deleted is worse than telling them nothing.
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Empty(t, env.sender.templates())
		assert.Equal(t, "superseded", env.tracker.last().props["outcome"])
	})

	t.Run("restore notice redelivered after a second deletion", func(t *testing.T) {
		env := deletionEnv(30)
		env.repo.deletedMentors["m1"] = deletedTestMentor("m1")

		w := env.do(http.MethodPost, "/jobs/profile-restored?mentorId=m1", nil)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Empty(t, env.sender.templates())
		assert.Equal(t, "superseded", env.tracker.last().props["outcome"])
	})
}

func TestProfileDeletionNoticesRequireAMentorID(t *testing.T) {
	for _, path := range []string{"/jobs/profile-deleted", "/jobs/profile-restored"} {
		env := deletionEnv(30)

		w := env.do(http.MethodPost, path, nil)

		assert.Equal(t, http.StatusBadRequest, w.Code, path)
		assert.Empty(t, env.sender.templates(), path)
	}
}

// Already purged: the retention window closed before the trigger was retried.
// Nothing to notify, and nothing an operator can fix.
func TestProfileDeletedMentorAlreadyGone(t *testing.T) {
	env := deletionEnv(30)

	w := env.do(http.MethodPost, "/jobs/profile-deleted?mentorId=absent", nil)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Empty(t, env.sender.templates())
	assert.Equal(t, "mentor_not_found", env.tracker.last().props["outcome"])
}

func TestProfileDeletedMentorWithoutEmail(t *testing.T) {
	env := deletionEnv(30)
	mentor := deletedTestMentor("m1")
	mentor.Email = ""
	env.repo.deletedMentors["m1"] = mentor

	w := env.do(http.MethodPost, "/jobs/profile-deleted?mentorId=m1", nil)

	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
	assert.Empty(t, env.sender.templates())
	assert.Equal(t, "missing_email", env.tracker.last().props["outcome"])
}

// A send failure must report itself: the deletion already committed, so a
// silent 200 here would leave a mentor un-notified with nothing in the metrics.
func TestProfileDeletedReportsSendFailure(t *testing.T) {
	env := deletionEnv(30)
	env.repo.deletedMentors["m1"] = deletedTestMentor("m1")
	env.sender.failTemplates["profile-deleted"] = true

	w := env.do(http.MethodPost, "/jobs/profile-deleted?mentorId=m1", nil)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Equal(t, "error", env.tracker.last().props["outcome"])
	assert.Equal(t, false, env.tracker.last().props["notified"])
}
