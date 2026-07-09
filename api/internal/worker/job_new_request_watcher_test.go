package worker

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openmentor-io/openmentor/api/pkg/analytics"
)

func testRequest(id, mentorID string) *JobRequest {
	return &JobRequest{
		ID:          id,
		MentorID:    mentorID,
		Name:        "Jane Mentee",
		Email:       "jane@example.com",
		Telegram:    "https://t.me/janementee",
		Description: "I want to grow",
		Level:       "Junior",
		Status:      "new",
	}
}

func TestNewRequestWatcherHappyPath(t *testing.T) {
	env := newJobsTestEnv()
	env.repo.mentors["m1"] = testMentor("m1")
	env.repo.requests["r1"] = testRequest("r1", "m1")

	w := env.do(http.MethodPost, "/jobs/new-request-watcher?requestId=r1", nil)

	assert.Equal(t, http.StatusOK, w.Code)

	// DB write: trimmed telegram + pending status.
	assert.Equal(t, map[string]string{"r1": "janementee"}, env.repo.requestUpdates)

	// Emails: mentee confirmation (no calendar), mentor, moderator.
	require.Equal(t, []string{"new-request", "new-request-mentor", "new-request-moderator"}, env.sender.templates())

	menteeMsg := env.sender.attempts[0]
	assert.Equal(t, "jane@example.com", menteeMsg.Recipient)
	assert.Equal(t, "Jane Mentee", menteeMsg.Props["first_name"])
	assert.Equal(t, "John Doe", menteeMsg.Props["mentor_name"])
	assert.Equal(t, "I want to grow", menteeMsg.Props["request_details"])
	assert.Equal(t, "$50", menteeMsg.Props["request_price"])
	assert.NotContains(t, menteeMsg.Props, "calendly_url")

	mentorMsg := env.sender.attempts[1]
	assert.Equal(t, "john@example.com", mentorMsg.Recipient)
	assert.Equal(t, "@janementee", mentorMsg.Props["mentee_tg"])
	assert.Equal(t, "jane@example.com", mentorMsg.Props["mentee_email"])
	assert.Equal(t, "I want to grow", mentorMsg.Props["mentee_request"])

	moderatorMsg := env.sender.attempts[2]
	assert.Equal(t, testModeratorsEmail, moderatorMsg.Recipient)
	assert.Equal(t, "Jane Mentee", moderatorMsg.Props["mentee_name"])
	assert.Equal(t, "Junior", moderatorMsg.Props["mentee_level"])

	event := env.tracker.last()
	require.NotNil(t, event)
	assert.Equal(t, analytics.EventNewRequestWatcherProcessed, event.event)
	assert.Equal(t, "request:r1", event.distinctID)
	assert.Equal(t, "success", event.props["outcome"])
	assert.Equal(t, false, event.props["mentor_calendar_enabled"])
}

func TestNewRequestWatcherCalendlyMentor(t *testing.T) {
	env := newJobsTestEnv()
	mentor := testMentor("m1")
	mentor.CalendarURL = "https://calendly.com/john"
	env.repo.mentors["m1"] = mentor
	request := testRequest("r1", "m1")
	request.Telegram = "" // optional handle not provided
	env.repo.requests["r1"] = request

	w := env.do(http.MethodGet, "/jobs/new-request-watcher?requestId=r1", nil)

	assert.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, []string{"new-request-calendly", "new-request-mentor", "new-request-moderator"}, env.sender.templates())
	assert.Equal(t, "https://calendly.com/john", env.sender.attempts[0].Props["calendly_url"])
	assert.Equal(t, "not provided", env.sender.attempts[1].Props["mentee_tg"])

	event := env.tracker.last()
	require.NotNil(t, event)
	assert.Equal(t, true, event.props["mentor_calendar_enabled"])
}

func TestNewRequestWatcherMissingRecords(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(env *jobsTestEnv)
		path        string
		wantOutcome string
	}{
		{
			name:        "missing requestId param",
			setup:       func(env *jobsTestEnv) {},
			path:        "/jobs/new-request-watcher",
			wantOutcome: "missing_request_id",
		},
		{
			name:        "request not found",
			setup:       func(env *jobsTestEnv) {},
			path:        "/jobs/new-request-watcher?requestId=ghost",
			wantOutcome: "request_not_found",
		},
		{
			name: "mentor not found",
			setup: func(env *jobsTestEnv) {
				env.repo.requests["r1"] = testRequest("r1", "ghost-mentor")
			},
			path:        "/jobs/new-request-watcher?requestId=r1",
			wantOutcome: "mentor_not_found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newJobsTestEnv()
			tt.setup(env)

			w := env.do(http.MethodPost, tt.path, nil)

			assert.Equal(t, http.StatusNotFound, w.Code)
			assert.Empty(t, env.sender.attempts)

			event := env.tracker.last()
			require.NotNil(t, event)
			assert.Equal(t, analytics.EventNewRequestWatcherProcessed, event.event)
			assert.Equal(t, tt.wantOutcome, event.props["outcome"])
		})
	}
}

func TestNewRequestWatcherEmailFailureResilience(t *testing.T) {
	env := newJobsTestEnv()
	env.repo.mentors["m1"] = testMentor("m1")
	env.repo.requests["r1"] = testRequest("r1", "m1")
	env.sender.failTemplates["new-request"] = true

	w := env.do(http.MethodPost, "/jobs/new-request-watcher?requestId=r1", nil)

	// The failed mentee send must not skip the mentor/moderator sends.
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Equal(t, []string{"new-request", "new-request-mentor", "new-request-moderator"}, env.sender.templates())

	event := env.tracker.last()
	require.NotNil(t, event)
	assert.Equal(t, "error", event.props["outcome"])
	assert.Equal(t, "email_send_failed", event.props["error_type"])
}
