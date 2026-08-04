package worker

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openmentor-io/openmentor/api/pkg/analytics"
	"github.com/openmentor-io/openmentor/api/pkg/tokenhash"
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
	assert.NotContains(t, *update.EmailConfirmationToken, "mcf_",
		"the row must hold the HASH, not the token that went into the email (D57)")
	require.NotNil(t, update.EmailConfirmationExpiresAt)
	assert.WithinDuration(t, time.Now().Add(24*time.Hour), *update.EmailConfirmationExpiresAt, time.Minute)

	// Email: only the confirmation request — the mentor welcome and the
	// moderator notification move to the mentor-confirmed job.
	require.Equal(t, []string{"mentor-confirm-email"}, env.sender.templates())
	confirmMsg := env.sender.attempts[0]
	assert.Equal(t, "john@example.com", confirmMsg.Recipient)
	assert.Equal(t, "John Doe", confirmMsg.Props["first_name"])
	// The emailed link carries the PLAINTEXT token and the row holds its hash:
	// asserting the relationship rather than either value is what would catch the
	// row and the email drifting apart, which is the one way this split breaks.
	emailedToken := strings.TrimPrefix(
		confirmMsg.Props["confirm_url"].(string),
		"https://openmentor.io/mentor/confirm?token=")
	assert.True(t, strings.HasPrefix(emailedToken, "mcf_"), "emailed token = %q", emailedToken)
	assert.Equal(t, tokenhash.Hash(emailedToken), *update.EmailConfirmationToken,
		"the emailed token must hash to the value stored on the row")

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

func TestNewMentorWatcherEmailFailureLeavesRowReplayable(t *testing.T) {
	env := newJobsTestEnv()
	env.repo.mentors["m1"] = testMentor("m1")
	env.sender.failTemplates["mentor-confirm-email"] = true

	w := env.do(http.MethodPost, "/jobs/new-mentor-watcher?mentorId=m1", nil)

	// The row is claimed BEFORE the email — no mentor may receive a token that
	// was not stored — so a failed send has to hand the claim back: holding it
	// takes the mentor out of finalize-stuck-registrations' replay set until the
	// token expires 24h later.
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Equal(t, []string{"mentor-confirm-email"}, env.sender.templates())
	require.Len(t, env.repo.finalized, 1)
	require.Len(t, env.repo.released, 1, "the claim must be released so the cron replays the row")
	assert.Equal(t, env.repo.finalized[0], env.repo.released[0],
		"the release must name the exact claim it undoes, or it could revert somebody else's write")

	event := env.tracker.last()
	require.NotNil(t, event)
	assert.Equal(t, "error", event.props["outcome"])
	assert.Equal(t, "email_send_failed", event.props["error_type"])
}

// TestNewMentorWatcherClaimsOnTheTokenItRead: the claim is a compare-and-swap on
// the confirmation token, and the value it swaps against has to be the one THIS
// run read. Hand the repository an empty expectation instead and two concurrent
// finalizations both match, both write and both email — the older link already
// dead. (The SQL side of the claim is covered in repository_claim_db_test.go,
// which needs a real Postgres.)
func TestNewMentorWatcherClaimsOnTheTokenItRead(t *testing.T) {
	env := newJobsTestEnv()
	mentor := testMentor("m1")
	mentor.EmailConfirmationToken = "expired-token-from-a-previous-pass"
	env.repo.mentors["m1"] = mentor

	w := env.do(http.MethodPost, "/jobs/new-mentor-watcher?mentorId=m1", nil)

	assert.Equal(t, http.StatusOK, w.Code)
	require.Len(t, env.repo.finalized, 1)
	assert.Equal(t, "expired-token-from-a-previous-pass",
		env.repo.finalized[0].ExpectedEmailConfirmationToken)
	require.NotNil(t, env.repo.finalized[0].EmailConfirmationToken)
	assert.NotEqual(t, "expired-token-from-a-previous-pass",
		*env.repo.finalized[0].EmailConfirmationToken, "the replay issues a fresh token")
}

// TestNewMentorWatcherSkipsEmailWhenRegistrationLeftDraft: the finalization
// UPDATE is guarded by WHERE status = 'draft', so a mentor who confirms — or a
// moderator who acts — between the read and the write leaves it matching no row.
// The confirmation token in hand was then never stored, and emailing it would
// send a link that is dead on arrival. A claim lost to a concurrent pass looks
// identical from here and must behave the same way: the winner owns the email.
func TestNewMentorWatcherSkipsEmailWhenRegistrationLeftDraft(t *testing.T) {
	env := newJobsTestEnv()
	env.repo.mentors["m1"] = testMentor("m1")
	env.repo.finalizeNotApplied = true

	w := env.do(http.MethodPost, "/jobs/new-mentor-watcher?mentorId=m1", nil)

	assert.Equal(t, http.StatusOK, w.Code, "an idempotent no-op, not a failure worth retrying")
	assert.Empty(t, env.sender.attempts, "no email may go out for a token that was not stored")
	assert.Empty(t, env.repo.released, "nothing was claimed, so nothing may be released")

	event := env.tracker.last()
	require.NotNil(t, event)
	assert.Equal(t, "superseded", event.props["outcome"])
}
