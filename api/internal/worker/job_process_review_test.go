package worker

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openmentor-io/openmentor/api/pkg/analytics"
)

func TestProcessMenteeReviewHappyPath(t *testing.T) {
	env := newJobsTestEnv()
	env.repo.mentors["m1"] = testMentor("m1")
	env.repo.reviews["rev1"] = &JobReview{
		ID:         "rev1",
		RequestID:  "r1",
		MentorID:   "m1",
		MenteeName: "Jane Mentee",
		ReviewText: "Great session!",
	}

	w := env.do(http.MethodGet, "/jobs/process-mentee-review?reviewId=rev1", nil)

	assert.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, []string{"new-review"}, env.sender.templates())
	msg := env.sender.attempts[0]
	assert.Equal(t, "john@example.com", msg.Recipient)
	assert.Equal(t, "John Doe", msg.Props["first_name"])
	assert.Equal(t, "Jane Mentee", msg.Props["mentee_name"])
	assert.Equal(t, "Great session!", msg.Props["review_text"])

	event := env.tracker.last()
	require.NotNil(t, event)
	assert.Equal(t, analytics.EventReviewSubmitted, event.event)
	assert.Equal(t, "request:r1", event.distinctID)
	assert.Equal(t, "success", event.props["outcome"])
	assert.Equal(t, "rev1", event.props["review_id"])
	assert.Equal(t, "m1", event.props["mentor_id"])
}

func TestProcessMenteeReviewTruncatesLongReview(t *testing.T) {
	env := newJobsTestEnv()
	env.repo.mentors["m1"] = testMentor("m1")
	env.repo.reviews["rev1"] = &JobReview{
		ID: "rev1", RequestID: "r1", MentorID: "m1", MenteeName: "Jane",
		ReviewText: strings.Repeat("x", 600),
	}

	w := env.do(http.MethodPost, "/jobs/process-mentee-review?reviewId=rev1", nil)

	assert.Equal(t, http.StatusOK, w.Code)
	require.Len(t, env.sender.attempts, 1)
	reviewText, ok := env.sender.attempts[0].Props["review_text"].(string)
	require.True(t, ok)
	assert.Equal(t, strings.Repeat("x", 500)+"...", reviewText)
}

func TestProcessMenteeReviewMissingRecords(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(env *jobsTestEnv)
		path        string
		wantOutcome string
	}{
		{
			name:        "missing reviewId param",
			setup:       func(env *jobsTestEnv) {},
			path:        "/jobs/process-mentee-review",
			wantOutcome: "missing_review_id",
		},
		{
			name:        "review not found",
			setup:       func(env *jobsTestEnv) {},
			path:        "/jobs/process-mentee-review?reviewId=ghost",
			wantOutcome: "review_not_found",
		},
		{
			name: "mentor not found",
			setup: func(env *jobsTestEnv) {
				env.repo.reviews["rev1"] = &JobReview{ID: "rev1", RequestID: "r1", MentorID: "ghost"}
			},
			path:        "/jobs/process-mentee-review?reviewId=rev1",
			wantOutcome: "mentor_not_found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newJobsTestEnv()
			tt.setup(env)

			w := env.do(http.MethodGet, tt.path, nil)

			assert.Equal(t, http.StatusNotFound, w.Code)
			assert.Empty(t, env.sender.attempts)

			event := env.tracker.last()
			require.NotNil(t, event)
			assert.Equal(t, analytics.EventReviewSubmitted, event.event)
			assert.Equal(t, tt.wantOutcome, event.props["outcome"])
		})
	}
}

func TestProcessMenteeReviewEmailFailure(t *testing.T) {
	env := newJobsTestEnv()
	env.repo.mentors["m1"] = testMentor("m1")
	env.repo.reviews["rev1"] = &JobReview{ID: "rev1", RequestID: "r1", MentorID: "m1", MenteeName: "Jane"}
	env.sender.failAll = true

	w := env.do(http.MethodGet, "/jobs/process-mentee-review?reviewId=rev1", nil)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	event := env.tracker.last()
	require.NotNil(t, event)
	assert.Equal(t, "error", event.props["outcome"])
	assert.Equal(t, "email_send_failed", event.props["error_type"])
}
