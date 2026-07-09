package worker

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openmentor-io/openmentor/api/pkg/analytics"
)

func TestMentorLoginEmailHappyPath(t *testing.T) {
	env := newJobsTestEnv()
	env.repo.mentors["m1"] = testMentor("m1")

	body := []byte(`{"type":"mentor_login","mentor_id":"m1","login_url":"https://openmentor.io/mentor/auth/callback?token=t"}`)
	w := env.do(http.MethodPost, "/jobs/mentor-login-email", body)

	assert.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, []string{"mentor-login"}, env.sender.templates())
	msg := env.sender.attempts[0]
	assert.Equal(t, "john@example.com", msg.Recipient)
	assert.Equal(t, "John Doe", msg.Props["mentor_name"])
	assert.Equal(t, "https://openmentor.io/mentor/auth/callback?token=t", msg.Props["login_url"])

	event := env.tracker.last()
	require.NotNil(t, event)
	assert.Equal(t, analytics.EventMentorAuthLoginEmailSent, event.event)
	assert.Equal(t, "mentor:m1", event.distinctID)
	assert.Equal(t, "success", event.props["outcome"])
	assert.Equal(t, 1, event.props["delivery_channels"])
}

func TestMentorLoginEmailInvalidPayloads(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		wantOutcome string
	}{
		{"empty body", ``, "invalid_payload_type"},
		{"wrong type", `{"type":"admin_login","mentor_id":"m1","login_url":"u"}`, "invalid_payload_type"},
		{"missing mentor_id", `{"type":"mentor_login","login_url":"u"}`, "missing_mentor_id"},
		{"missing login_url", `{"type":"mentor_login","mentor_id":"m1"}`, "missing_login_url"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newJobsTestEnv()
			env.repo.mentors["m1"] = testMentor("m1")

			w := env.do(http.MethodPost, "/jobs/mentor-login-email", []byte(tt.body))

			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Empty(t, env.sender.attempts)

			event := env.tracker.last()
			require.NotNil(t, event)
			assert.Equal(t, tt.wantOutcome, event.props["outcome"])
		})
	}
}

func TestMentorLoginEmailMentorNotFound(t *testing.T) {
	env := newJobsTestEnv()

	body := []byte(`{"type":"mentor_login","mentor_id":"ghost","login_url":"u"}`)
	w := env.do(http.MethodPost, "/jobs/mentor-login-email", body)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Empty(t, env.sender.attempts)

	event := env.tracker.last()
	require.NotNil(t, event)
	assert.Equal(t, "mentor_not_found", event.props["outcome"])
}

func TestMentorLoginEmailSendFailure(t *testing.T) {
	env := newJobsTestEnv()
	env.repo.mentors["m1"] = testMentor("m1")
	env.sender.failAll = true

	body := []byte(`{"type":"mentor_login","mentor_id":"m1","login_url":"u"}`)
	w := env.do(http.MethodPost, "/jobs/mentor-login-email", body)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	event := env.tracker.last()
	require.NotNil(t, event)
	assert.Equal(t, "error", event.props["outcome"])
	assert.Equal(t, "email_send_failed", event.props["error_type"])
}

func TestModeratorLoginEmailByID(t *testing.T) {
	env := newJobsTestEnv()
	env.repo.moderators["mod1"] = &JobModerator{ID: "mod1", Name: "Alice Admin", Email: "alice@openmentor.io"}

	body := []byte(`{"type":"admin_login","moderator_id":"mod1","login_url":"https://openmentor.io/admin/auth/callback?token=t"}`)
	w := env.do(http.MethodPost, "/jobs/moderator-login-email", body)

	assert.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, []string{"mentor-login"}, env.sender.templates())
	msg := env.sender.attempts[0]
	assert.Equal(t, "alice@openmentor.io", msg.Recipient)
	assert.Equal(t, "Alice Admin", msg.Props["mentor_name"])

	event := env.tracker.last()
	require.NotNil(t, event)
	assert.Equal(t, analytics.EventAdminAuthLoginEmailSent, event.event)
	assert.Equal(t, "moderator:mod1", event.distinctID)
	assert.Equal(t, "success", event.props["outcome"])
}

func TestModeratorLoginEmailByEmailWithDefaultName(t *testing.T) {
	env := newJobsTestEnv()

	body := []byte(`{"type":"admin_login","moderator_email":"mods@openmentor.io","login_url":"u"}`)
	w := env.do(http.MethodPost, "/jobs/moderator-login-email", body)

	assert.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, []string{"mentor-login"}, env.sender.templates())
	msg := env.sender.attempts[0]
	assert.Equal(t, "mods@openmentor.io", msg.Recipient)
	assert.Equal(t, "moderator", msg.Props["mentor_name"], "name defaults to 'moderator'")
}

func TestModeratorLoginEmailInvalidAndMissing(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		wantCode    int
		wantOutcome string
	}{
		{"wrong type", `{"type":"mentor_login","login_url":"u"}`, http.StatusBadRequest, "invalid_payload_type"},
		{"missing login_url", `{"type":"admin_login","moderator_email":"a@b.c"}`, http.StatusBadRequest, "missing_login_url"},
		{"moderator not found", `{"type":"admin_login","moderator_id":"ghost","login_url":"u"}`, http.StatusNotFound, "moderator_not_found"},
		{"missing email", `{"type":"admin_login","login_url":"u"}`, http.StatusBadRequest, "missing_moderator_email"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newJobsTestEnv()

			w := env.do(http.MethodPost, "/jobs/moderator-login-email", []byte(tt.body))

			assert.Equal(t, tt.wantCode, w.Code)
			assert.Empty(t, env.sender.attempts)

			event := env.tracker.last()
			require.NotNil(t, event)
			assert.Equal(t, analytics.EventAdminAuthLoginEmailSent, event.event)
			assert.Equal(t, tt.wantOutcome, event.props["outcome"])
		})
	}
}

func TestModeratorLoginEmailSendFailure(t *testing.T) {
	env := newJobsTestEnv()
	env.sender.failAll = true

	body := []byte(`{"type":"admin_login","moderator_email":"mods@openmentor.io","login_url":"u"}`)
	w := env.do(http.MethodPost, "/jobs/moderator-login-email", body)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	event := env.tracker.last()
	require.NotNil(t, event)
	assert.Equal(t, "error", event.props["outcome"])
	assert.Equal(t, "email_send_failed", event.props["error_type"])
}
