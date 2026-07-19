package worker

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openmentor-io/openmentor/api/pkg/analytics"
)

func moderationBody(action string) []byte {
	return []byte(fmt.Sprintf(
		`{"type":"mentor_moderation","mentor_id":"m1","action":"%s","moderator_id":"mod1","role":"admin"}`,
		action,
	))
}

func newModerationEnv(mentorStatus string) *jobsTestEnv {
	env := newJobsTestEnv()
	mentor := testMentor("m1")
	mentor.Status = mentorStatus
	env.repo.mentors["m1"] = mentor
	env.repo.moderators["mod1"] = &JobModerator{ID: "mod1", Name: "Alice Admin", Email: "alice@openmentor.io"}
	return env
}

func TestMentorModerationActionApprove(t *testing.T) {
	// The API already wrote status=active before firing the trigger:
	// the worker must NOT write status again, only send the email.
	env := newModerationEnv("active")

	w := env.do(http.MethodPost, "/jobs/mentor-moderation-action", moderationBody("approve"))

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, env.repo.statusUpdates, "status already matches: no double-write")

	require.Equal(t, []string{"new-mentor-approved"}, env.sender.templates())
	msg := env.sender.attempts[0]
	assert.Equal(t, "john@example.com", msg.Recipient)
	assert.Equal(t, "John Doe", msg.Props["first_name"])
	assert.Equal(t, "https://openmentor.io/mentor/john-doe-42", msg.Props["mentor_profile_url"])

	event := env.tracker.last()
	require.NotNil(t, event)
	assert.Equal(t, analytics.EventAdminMentorModerationAction, event.event)
	assert.Equal(t, "moderator:mod1", event.distinctID)
	assert.Equal(t, "success", event.props["outcome"])
	assert.Equal(t, "approve", event.props["action"])
	assert.Equal(t, "m1", event.props["target_mentor_id"])
	assert.Equal(t, "admin", event.props["moderator_role"])
}

func TestMentorModerationActionApproveInvitesToSlack(t *testing.T) {
	// A newly approved mentor is invited to the community Slack workspace
	// with the same email address the approval email went to.
	env := newModerationEnv("active")

	w := env.do(http.MethodPost, "/jobs/mentor-moderation-action", moderationBody("approve"))

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, []string{"john@example.com"}, env.slack.invited)
}

func TestMentorModerationActionSlackInviteFailureDoesNotFailJob(t *testing.T) {
	// The approval email is already out when the invite runs: a Slack
	// failure is logged but must not 5xx the job (the API would replay the
	// trigger and resend the email).
	env := newModerationEnv("active")
	env.slack.err = errors.New("slack down")

	w := env.do(http.MethodPost, "/jobs/mentor-moderation-action", moderationBody("approve"))

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, []string{"new-mentor-approved"}, env.sender.templates())
	assert.Equal(t, []string{"john@example.com"}, env.slack.invited, "invite must be attempted")
	assert.Equal(t, "success", env.tracker.last().props["outcome"])
}

func TestMentorModerationActionNilSlackInviterSkipsInvite(t *testing.T) {
	// Slack not configured (SLACK_ADMIN_TOKEN empty): approve works as
	// before, no invite attempted.
	env := newModerationEnv("active")
	env.handlers.slack = nil

	w := env.do(http.MethodPost, "/jobs/mentor-moderation-action", moderationBody("approve"))

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, []string{"new-mentor-approved"}, env.sender.templates())
}

func TestMentorModerationActionDecline(t *testing.T) {
	env := newModerationEnv("declined")

	w := env.do(http.MethodPost, "/jobs/mentor-moderation-action", moderationBody("decline"))

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, env.repo.statusUpdates)
	require.Equal(t, []string{"new-mentor-declined"}, env.sender.templates())
	assert.Equal(t, "John Doe", env.sender.attempts[0].Props["first_name"])
	assert.Empty(t, env.slack.invited, "only approvals invite to Slack")
}

func TestMentorModerationActionReturn(t *testing.T) {
	// The API already wrote status=draft + moderation_note before firing
	// the trigger: the worker only sends the new-mentor-returned email
	// with the reviewer's note and the profile editor link.
	env := newModerationEnv("draft")
	env.repo.mentors["m1"].ModerationNote = "Please add a real photo and expand the about section."

	w := env.do(http.MethodPost, "/jobs/mentor-moderation-action", moderationBody("return"))

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, env.repo.statusUpdates, "status already matches: no double-write")

	require.Equal(t, []string{"new-mentor-returned"}, env.sender.templates())
	msg := env.sender.attempts[0]
	assert.Equal(t, "john@example.com", msg.Recipient)
	assert.Equal(t, "John Doe", msg.Props["first_name"])
	assert.Equal(t, "Please add a real photo and expand the about section.", msg.Props["reviewer_note"])
	assert.Equal(t, "https://openmentor.io/mentor/profile/edit", msg.Props["edit_url"])
	assert.Empty(t, env.slack.invited, "only approvals invite to Slack")

	event := env.tracker.last()
	require.NotNil(t, event)
	assert.Equal(t, analytics.EventAdminMentorModerationAction, event.event)
	assert.Equal(t, "success", event.props["outcome"])
	assert.Equal(t, "return", event.props["action"])
}

func TestMentorModerationActionReturnRepairsStaleStatus(t *testing.T) {
	// A replayed 'return' trigger against a row still 'pending' repairs
	// the status to draft (the repository write is guarded in SQL against
	// ever-activated mentors) and still sends the email.
	env := newModerationEnv("pending")
	env.repo.mentors["m1"].ModerationNote = "Fix the photo."

	w := env.do(http.MethodPost, "/jobs/mentor-moderation-action", moderationBody("return"))

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, map[string]string{"m1": "draft"}, env.repo.statusUpdates)
	assert.Equal(t, []string{"new-mentor-returned"}, env.sender.templates())
}

func TestMentorModerationActionRepairsStaleStatus(t *testing.T) {
	// If the API's status write is missing/stale (e.g. replayed trigger),
	// the worker repairs it, logs a warning and still sends the email.
	env := newModerationEnv("pending")

	w := env.do(http.MethodPost, "/jobs/mentor-moderation-action", moderationBody("approve"))

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, map[string]string{"m1": "active"}, env.repo.statusUpdates)
	assert.Equal(t, []string{"new-mentor-approved"}, env.sender.templates())
}

func TestMentorModerationActionInvalidPayloads(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		wantCode    int
		wantOutcome string
	}{
		{"wrong type", `{"type":"nope","mentor_id":"m1","action":"approve","moderator_id":"mod1"}`, http.StatusBadRequest, "invalid_payload_type"},
		{"missing mentor_id", `{"type":"mentor_moderation","action":"approve","moderator_id":"mod1"}`, http.StatusBadRequest, "missing_mentor_id"},
		{"missing moderator_id", `{"type":"mentor_moderation","mentor_id":"m1","action":"approve"}`, http.StatusBadRequest, "missing_moderator_id"},
		{"invalid action", `{"type":"mentor_moderation","mentor_id":"m1","action":"promote","moderator_id":"mod1"}`, http.StatusBadRequest, "invalid_action"},
		{"moderator not found", `{"type":"mentor_moderation","mentor_id":"m1","action":"approve","moderator_id":"ghost"}`, http.StatusForbidden, "moderator_not_found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newModerationEnv("active")
			delete(env.repo.moderators, "ghost")

			w := env.do(http.MethodPost, "/jobs/mentor-moderation-action", []byte(tt.body))

			assert.Equal(t, tt.wantCode, w.Code)
			assert.Empty(t, env.sender.attempts)

			event := env.tracker.last()
			require.NotNil(t, event)
			assert.Equal(t, analytics.EventAdminMentorModerationAction, event.event)
			assert.Equal(t, tt.wantOutcome, event.props["outcome"])
		})
	}
}

func TestMentorModerationActionMentorNotFound(t *testing.T) {
	env := newJobsTestEnv()
	env.repo.moderators["mod1"] = &JobModerator{ID: "mod1", Name: "Alice Admin"}

	w := env.do(http.MethodPost, "/jobs/mentor-moderation-action", moderationBody("approve"))

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Empty(t, env.sender.attempts)

	event := env.tracker.last()
	require.NotNil(t, event)
	assert.Equal(t, "mentor_not_found", event.props["outcome"])
}

func TestMentorModerationActionEmailFailure(t *testing.T) {
	env := newModerationEnv("active")
	env.sender.failAll = true

	w := env.do(http.MethodPost, "/jobs/mentor-moderation-action", moderationBody("approve"))

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Equal(t, []string{"new-mentor-approved"}, env.sender.templates(), "send must be attempted")
	assert.Empty(t, env.slack.invited, "no Slack invite when the email failed (trigger will be replayed)")

	event := env.tracker.last()
	require.NotNil(t, event)
	assert.Equal(t, "error", event.props["outcome"])
}
