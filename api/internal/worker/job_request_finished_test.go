package worker

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openmentor-io/openmentor-api/pkg/analytics"
)

func finishedRequest(status string) *JobRequest {
	return &JobRequest{
		ID:         "r1",
		MentorID:   "m1",
		Name:       "Jane Mentee",
		Email:      "jane@example.com",
		Status:     status,
		MentorName: "John Doe",
	}
}

func TestRequestProcessFinishedDone(t *testing.T) {
	env := newJobsTestEnv()
	env.repo.requestsWithMentor["r1"] = finishedRequest("done")

	w := env.do(http.MethodGet, "/jobs/request-process-finished?requestId=r1", nil)

	assert.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, []string{"session-complete"}, env.sender.templates())
	msg := env.sender.attempts[0]
	assert.Equal(t, "jane@example.com", msg.Recipient)
	assert.Equal(t, "Jane Mentee", msg.Props["first_name"])
	assert.Equal(t, "John Doe", msg.Props["mentor_name"])
	assert.Equal(t, "r1", msg.Props["request_id"])

	event := env.tracker.last()
	require.NotNil(t, event)
	assert.Equal(t, analytics.EventRequestProcessFinishedNotified, event.event)
	assert.Equal(t, "request:r1", event.distinctID)
	assert.Equal(t, "success", event.props["outcome"])
	assert.Equal(t, "done", event.props["status"])
}

func TestRequestProcessFinishedDeclinedWithReason(t *testing.T) {
	env := newJobsTestEnv()
	request := finishedRequest("declined")
	request.DeclineReason = "no_time"
	request.DeclineComment = "Try again <soon>"
	env.repo.requestsWithMentor["r1"] = request

	w := env.do(http.MethodGet, "/jobs/request-process-finished?requestId=r1", nil)

	assert.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, []string{"session-declined"}, env.sender.templates())
	msg := env.sender.attempts[0]

	declineInfo, ok := msg.Props["decline_info"].(string)
	require.True(t, ok)
	assert.Contains(t, declineInfo, "<strong>Reason:</strong> No time at the moment")
	assert.Contains(t, declineInfo, "<strong>Comment:</strong> Try again &lt;soon&gt;", "comment must be HTML-escaped")

	declineText, ok := msg.Props["decline_info_text"].(string)
	require.True(t, ok)
	assert.Contains(t, declineText, "Reason: No time at the moment")
	assert.Contains(t, declineText, "Comment: Try again <soon>")
}

func TestRequestProcessFinishedDeclinedWithoutReason(t *testing.T) {
	env := newJobsTestEnv()
	env.repo.requestsWithMentor["r1"] = finishedRequest("declined")

	w := env.do(http.MethodGet, "/jobs/request-process-finished?requestId=r1", nil)

	assert.Equal(t, http.StatusOK, w.Code)
	require.Len(t, env.sender.attempts, 1)
	assert.Equal(t, defaultDeclineReasonText, env.sender.attempts[0].Props["decline_info"])
	assert.Equal(t, defaultDeclineReasonText, env.sender.attempts[0].Props["decline_info_text"])
}

func TestRequestProcessFinishedNonActionableStatus(t *testing.T) {
	env := newJobsTestEnv()
	env.repo.requestsWithMentor["r1"] = finishedRequest("pending")

	w := env.do(http.MethodGet, "/jobs/request-process-finished?requestId=r1", nil)

	// Mirrors the func: non-final statuses are a tracked no-op with a 200.
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, env.sender.attempts)

	event := env.tracker.last()
	require.NotNil(t, event)
	assert.Equal(t, "status_not_actionable", event.props["outcome"])
	assert.Equal(t, "pending", event.props["status"])
}

func TestRequestProcessFinishedMissingRecords(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		wantCode    int
		wantOutcome string
	}{
		// Mirrors the func: missing param is a 400, unknown record a 404.
		{"missing requestId param", "/jobs/request-process-finished", http.StatusBadRequest, "missing_request_id"},
		{"request not found", "/jobs/request-process-finished?requestId=ghost", http.StatusNotFound, "request_not_found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newJobsTestEnv()

			w := env.do(http.MethodGet, tt.path, nil)

			assert.Equal(t, tt.wantCode, w.Code)
			assert.Empty(t, env.sender.attempts)

			event := env.tracker.last()
			require.NotNil(t, event)
			assert.Equal(t, analytics.EventRequestProcessFinishedNotified, event.event)
			assert.Equal(t, tt.wantOutcome, event.props["outcome"])
		})
	}
}

func TestRequestProcessFinishedEmailFailure(t *testing.T) {
	env := newJobsTestEnv()
	env.repo.requestsWithMentor["r1"] = finishedRequest("done")
	env.sender.failAll = true

	w := env.do(http.MethodGet, "/jobs/request-process-finished?requestId=r1", nil)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	event := env.tracker.last()
	require.NotNil(t, event)
	assert.Equal(t, "error", event.props["outcome"])
	assert.Equal(t, "email_send_failed", event.props["error_type"])
}
