package worker

import (
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openmentor-io/openmentor/api/pkg/analytics"
	"github.com/openmentor-io/openmentor/api/pkg/email/templates"
	"github.com/openmentor-io/openmentor/api/pkg/reviewtoken"
)

// finishedRequestID is uuid-shaped rather than "r1" because the assertions below
// check that the request id does not appear ANYWHERE in the review link — and a
// two-character id collides with random base64url roughly once in a hundred
// runs, which is a flaky test that fails on the one assertion that must not be
// flaky. Production ids are uuids; the fixture now matches.
const finishedRequestID = "9f1b2c3d-4e5f-4a6b-8c7d-0e1f2a3b4c5d"

func finishedRequest(status string) *JobRequest {
	return &JobRequest{
		ID:         finishedRequestID,
		MentorID:   "m1",
		Name:       "Jane Mentee",
		Email:      "jane@example.com",
		Status:     status,
		MentorName: "John Doe",
	}
}

func TestRequestProcessFinishedDone(t *testing.T) {
	env := newJobsTestEnv()
	env.repo.requestsWithMentor[finishedRequestID] = finishedRequest("done")

	w := env.do(http.MethodGet, "/jobs/request-process-finished?requestId="+finishedRequestID, nil)

	assert.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, []string{"session-complete"}, env.sender.templates())
	msg := env.sender.attempts[0]
	assert.Equal(t, "jane@example.com", msg.Recipient)
	assert.Equal(t, "Jane Mentee", msg.Props["first_name"])
	assert.Equal(t, "John Doe", msg.Props["mentor_name"])
	assertReviewInvitationLink(t, env, msg.Props)

	event := env.tracker.last()
	require.NotNil(t, event)
	assert.Equal(t, analytics.EventRequestProcessFinishedNotified, event.event)
	assert.Equal(t, "mentor:m1", event.distinctID, "events must be keyed by mentor, never by the request capability (P14)")
	assert.Equal(t, "success", event.props["outcome"])
	assert.Equal(t, "done", event.props["status"])
}

func TestRequestProcessFinishedDeclinedWithReason(t *testing.T) {
	env := newJobsTestEnv()
	request := finishedRequest("declined")
	request.DeclineReason = "no_time"
	request.DeclineComment = "Try again <soon>"
	env.repo.requestsWithMentor[finishedRequestID] = request

	w := env.do(http.MethodGet, "/jobs/request-process-finished?requestId="+finishedRequestID, nil)

	assert.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, []string{"session-declined"}, env.sender.templates())
	msg := env.sender.attempts[0]

	declineInfo := htmlProp(t, msg.Props, "decline_info")
	assert.Contains(t, declineInfo, "<strong>Reason:</strong> No time at the moment")
	assert.Contains(t, declineInfo, "<strong>Comment:</strong> Try again &lt;soon&gt;", "comment must be HTML-escaped")

	declineText, ok := msg.Props["decline_info_text"].(string)
	require.True(t, ok)
	assert.Contains(t, declineText, "Reason: No time at the moment")
	assert.Contains(t, declineText, "Comment: Try again <soon>")
}

func TestRequestProcessFinishedDeclinedWithoutReason(t *testing.T) {
	env := newJobsTestEnv()
	env.repo.requestsWithMentor[finishedRequestID] = finishedRequest("declined")

	w := env.do(http.MethodGet, "/jobs/request-process-finished?requestId="+finishedRequestID, nil)

	assert.Equal(t, http.StatusOK, w.Code)
	require.Len(t, env.sender.attempts, 1)
	assert.Equal(t, template.HTML(defaultDeclineReasonText), env.sender.attempts[0].Props["decline_info"])
	assert.Equal(t, defaultDeclineReasonText, env.sender.attempts[0].Props["decline_info_text"])
}

func TestRequestProcessFinishedNonActionableStatus(t *testing.T) {
	env := newJobsTestEnv()
	env.repo.requestsWithMentor[finishedRequestID] = finishedRequest("pending")

	w := env.do(http.MethodGet, "/jobs/request-process-finished?requestId="+finishedRequestID, nil)

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
	env.repo.requestsWithMentor[finishedRequestID] = finishedRequest("done")
	env.sender.failAll = true

	w := env.do(http.MethodGet, "/jobs/request-process-finished?requestId="+finishedRequestID, nil)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	event := env.tracker.last()
	require.NotNil(t, event)
	assert.Equal(t, "error", event.props["outcome"])
	assert.Equal(t, "email_send_failed", event.props["error_type"])
}

// assertReviewInvitationLink is the H4 sentinel on the outgoing email: the link
// must carry the capability in a URL FRAGMENT and nowhere else, and what got
// stored must be the token's hash rather than the token.
func assertReviewInvitationLink(t *testing.T, env *jobsTestEnv, props map[string]interface{}) string {
	t.Helper()

	reviewURL, ok := props["review_url"].(string)
	require.True(t, ok, "session-complete must carry review_url")

	parsed, err := url.Parse(reviewURL)
	require.NoError(t, err)
	assert.Equal(t, "/reviews/new", parsed.Path, "the capability must not be a path segment")
	assert.Empty(t, parsed.RawQuery, "the capability must not be a query parameter")

	raw := parsed.Fragment[len(ReviewTokenFragmentKey)+1:]
	require.True(t, strings.HasPrefix(parsed.Fragment, ReviewTokenFragmentKey+"="), "fragment = %q", parsed.Fragment)
	assert.True(t, reviewtoken.WellFormed(raw), "the fragment must carry a well-formed token")

	require.Len(t, env.repo.reviewInvitations, 1)
	stored := env.repo.reviewInvitations[0]
	assert.Equal(t, reviewtoken.Hash(raw), stored.tokenHash, "only the hash may be persisted")
	assert.NotContains(t, stored.tokenHash, raw, "the raw token must not be recoverable from storage")
	assert.WithinDuration(t, time.Now().Add(reviewtoken.TTL), stored.expiresAt, time.Minute)

	// The request id it authorizes stays internal: it is not in the link.
	assert.NotContains(t, reviewURL, stored.requestID)
	return raw
}

// TestRequestProcessFinishedMintsAFreshCapabilityPerSend pins the append
// semantics that keep a resend from invalidating the link already in the
// mentee's inbox: two sends produce two DIFFERENT tokens and two stored rows,
// and neither overwrites the other.
func TestRequestProcessFinishedMintsAFreshCapabilityPerSend(t *testing.T) {
	env := newJobsTestEnv()
	env.repo.requestsWithMentor[finishedRequestID] = finishedRequest("done")

	require.Equal(t, http.StatusOK, env.do(http.MethodGet, "/jobs/request-process-finished?requestId="+finishedRequestID, nil).Code)
	require.Equal(t, http.StatusOK, env.do(http.MethodGet, "/jobs/request-process-finished?requestId="+finishedRequestID, nil).Code)

	require.Len(t, env.repo.reviewInvitations, 2)
	first, second := env.repo.reviewInvitations[0], env.repo.reviewInvitations[1]
	assert.NotEqual(t, first.tokenHash, second.tokenHash, "each send must mint fresh entropy")
	assert.Equal(t, first.requestID, second.requestID)

	require.Len(t, env.sender.attempts, 2)
	firstLink, ok := env.sender.attempts[0].Props["review_url"].(string)
	require.True(t, ok)
	secondLink, ok := env.sender.attempts[1].Props["review_url"].(string)
	require.True(t, ok)
	assert.NotEqual(t, firstLink, secondLink)
}

// TestRequestProcessFinishedNeverMailsAnUnstoredToken is the ordering guarantee:
// if the invitation cannot be persisted, no email goes out. A mailed token that
// was never stored is a link that can never be spent.
func TestRequestProcessFinishedNeverMailsAnUnstoredToken(t *testing.T) {
	env := newJobsTestEnv()
	env.repo.requestsWithMentor[finishedRequestID] = finishedRequest("done")
	env.repo.reviewInvitationErr = errors.New("insert failed")

	w := env.do(http.MethodGet, "/jobs/request-process-finished?requestId="+finishedRequestID, nil)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Empty(t, env.sender.attempts, "no email may be sent when the capability was not stored")

	event := env.tracker.last()
	require.NotNil(t, event)
	assert.Equal(t, "error", event.props["outcome"])
	assert.Equal(t, errTypeDBError, event.props["error_type"])
}

// TestWorkerInvitationInsertRejectsARawTokenBeforeTheDatabase pins the parity
// that made the worker delegate to internal/repository instead of keeping its
// own copy of the INSERT: minting happens here, so the pre-database guard has to
// cover here too.
//
// The pool is deliberately nil — reaching Postgres would panic, so passing means
// the raw token was rejected before any statement was sent, rather than by the
// CHECK constraint at the far end.
func TestWorkerInvitationInsertRejectsARawTokenBeforeTheDatabase(t *testing.T) {
	raw, hash, err := reviewtoken.New()
	require.NoError(t, err)
	require.NotEqual(t, raw, hash)

	repo := NewRepository(nil)
	assert.Error(t, repo.CreateReviewInvitation(t.Context(),
		finishedRequestID, raw, time.Now().Add(reviewtoken.TTL)),
		"a raw token passed where a digest belongs must not reach the database")
}

// TestSessionCompleteEmailCarriesNoRequestID is the sentinel on the rendered
// email: the pre-H4 template interpolated client_requests.id into the review
// link, which is what made the primary key a bearer capability.
func TestSessionCompleteEmailCarriesNoRequestID(t *testing.T) {
	env := newJobsTestEnv()
	request := finishedRequest("done")
	env.repo.requestsWithMentor[request.ID] = request

	w := env.do(http.MethodGet, "/jobs/request-process-finished?requestId="+request.ID, nil)
	require.Equal(t, http.StatusOK, w.Code)

	rendered, err := templates.Render("session-complete", env.sender.attempts[0].Props)
	require.NoError(t, err)
	for part, body := range map[string]string{"html": rendered.HTML, "text": rendered.Text} {
		assert.NotContains(t, body, request.ID, "%s part still carries the request id", part)
		assert.NotContains(t, body, "request_id", "%s part still references request_id", part)
	}
}
