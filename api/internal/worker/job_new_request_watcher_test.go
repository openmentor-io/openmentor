package worker

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openmentor-io/openmentor/api/pkg/analytics"
	"github.com/openmentor-io/openmentor/api/pkg/email/templates"
)

// assertTemplatePropsSatisfied checks that every email the job sent supplies a
// non-empty value for each {{placeholder}} its template declares. Rendering
// under missingkey=error already fails an absent prop, but only at send time
// and not for one that is present and empty — which still ships href="".
func assertTemplatePropsSatisfied(t *testing.T, env *jobsTestEnv) {
	t.Helper()

	for _, attempt := range env.sender.attempts {
		tpl, err := templates.GetTemplate(attempt.TemplateName)
		require.NoError(t, err, "template %q must exist", attempt.TemplateName)

		for _, placeholder := range templates.Placeholders(tpl) {
			value, ok := attempt.Props[placeholder]
			assert.Truef(t, ok, "template %q uses {{%s}} but the job supplies no such prop", attempt.TemplateName, placeholder)
			assert.NotEmptyf(t, value, "template %q supplies an empty {{%s}}", attempt.TemplateName, placeholder)
		}
	}
}

func testRequest(id, mentorID string) *JobRequest {
	return &JobRequest{
		ID:               id,
		MentorID:         mentorID,
		Name:             "Jane Mentee",
		Email:            "jane@example.com",
		PreferredContact: " linkedin.com/in/janementee ", // free text, only whitespace is trimmed
		Description:      "I want to grow",
		Level:            "Junior",
		Status:           "new",
	}
}

func TestNewRequestWatcherHappyPath(t *testing.T) {
	env := newJobsTestEnv()
	env.repo.mentors["m1"] = testMentor("m1")
	env.repo.requests["r1"] = testRequest("r1", "m1")

	w := env.do(http.MethodPost, "/jobs/new-request-watcher?requestId=r1", nil)

	assert.Equal(t, http.StatusOK, w.Code)

	// DB write: trimmed contact details + pending status.
	assert.Equal(t, map[string]string{"r1": "linkedin.com/in/janementee"}, env.repo.requestUpdates)

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
	assert.Equal(t, "linkedin.com/in/janementee", mentorMsg.Props["mentee_contact"])
	assert.Equal(t, "jane@example.com", mentorMsg.Props["mentee_email"])
	assert.Equal(t, "I want to grow", mentorMsg.Props["mentee_request"])
	// The CTA deep-links to the request itself, not the portal landing page.
	assert.Equal(t, "https://openmentor.io/mentor/requests/r1", mentorMsg.Props["request_url"])

	moderatorMsg := env.sender.attempts[2]
	assert.Equal(t, testModeratorsEmail, moderatorMsg.Recipient)
	assert.Equal(t, "Jane Mentee", moderatorMsg.Props["mentee_name"])
	assert.Equal(t, "Junior", moderatorMsg.Props["mentee_level"])
	assert.Equal(t, "https://openmentor.io/admin/mentors/m1/requests/r1", moderatorMsg.Props["request_url"])
	assert.Equal(t, "https://openmentor.io/admin", moderatorMsg.Props["admin_url"])

	// Every {{placeholder}} in each template must be supplied, or SES renders
	// it empty — an anchor with an empty href, which is how a "correct link"
	// silently becomes a dead one.
	assertTemplatePropsSatisfied(t, env)

	event := env.tracker.last()
	require.NotNil(t, event)
	assert.Equal(t, analytics.EventNewRequestWatcherProcessed, event.event)
	assert.Equal(t, "mentor:m1", event.distinctID, "events must be keyed by mentor, never by the request capability (P14)")
	assert.Equal(t, "success", event.props["outcome"])
	assert.Equal(t, false, event.props["mentor_calendar_enabled"])
}

func TestNewRequestWatcherCalendlyMentor(t *testing.T) {
	env := newJobsTestEnv()
	mentor := testMentor("m1")
	mentor.CalendarURL = "https://calendly.com/john"
	env.repo.mentors["m1"] = mentor
	request := testRequest("r1", "m1")
	request.PreferredContact = "" // optional contact details not provided
	env.repo.requests["r1"] = request

	w := env.do(http.MethodGet, "/jobs/new-request-watcher?requestId=r1", nil)

	assert.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, []string{"new-request-calendly", "new-request-mentor", "new-request-moderator"}, env.sender.templates())
	assert.Equal(t, "https://calendly.com/john", env.sender.attempts[0].Props["calendly_url"])
	assert.Equal(t, "not provided", env.sender.attempts[1].Props["mentee_contact"])

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

// TestNewRequestWatcherReplayEmailsNobody: the callback delivered twice must not
// announce the same request twice. The claim inside SetRequestContactPending is
// what decides, so the second call reaches no email and no write.
func TestNewRequestWatcherReplayEmailsNobody(t *testing.T) {
	env := newJobsTestEnv()
	env.repo.mentors["m1"] = testMentor("m1")
	env.repo.requests["r1"] = testRequest("r1", "m1")

	first := env.do(http.MethodPost, "/jobs/new-request-watcher?requestId=r1", nil)
	require.Equal(t, http.StatusOK, first.Code)
	require.Len(t, env.sender.attempts, 3)

	second := env.do(http.MethodPost, "/jobs/new-request-watcher?requestId=r1", nil)

	// 200 with superseded: a replay is acknowledged, not retried.
	assert.Equal(t, http.StatusOK, second.Code)
	assert.Contains(t, second.Body.String(), `"superseded":true`)
	assert.Len(t, env.sender.attempts, 3, "a replayed callback must email nobody a second time")
	assert.Equal(t, 1, env.repo.requestClaims["r1"], "the request may be claimed exactly once")

	event := env.tracker.last()
	require.NotNil(t, event)
	assert.Equal(t, analytics.EventNewRequestWatcherProcessed, event.event)
	assert.Equal(t, "superseded", event.props["outcome"])
	assert.Equal(t, "system:worker", event.distinctID, "no mentor is involved in a no-op")
}

// TestNewRequestWatcherAdvancedRequestEmailsNobody is the same guard for the
// worse case: the request has already been declined or completed, so the blind
// status = 'pending' write would have REOPENED it and then told the mentee, the
// mentor and the moderators that a new request was waiting.
func TestNewRequestWatcherAdvancedRequestEmailsNobody(t *testing.T) {
	env := newJobsTestEnv()
	env.repo.mentors["m1"] = testMentor("m1")
	env.repo.requests["r1"] = testRequest("r1", "m1")
	env.repo.requestAlreadyClaimed = true

	w := env.do(http.MethodPost, "/jobs/new-request-watcher?requestId=r1", nil)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, env.sender.attempts)
	assert.Empty(t, env.repo.requestUpdates, "no write for a request that has moved on")
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
