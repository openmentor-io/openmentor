package worker

import (
	"context"
	"net/http"
	"testing"

	"github.com/openmentor-io/openmentor/api/config"
)

// TestFinalizeStuckRegistrationsConvergesDroppedTrigger is the P2 lockout in
// test form: the API's finalization trigger is a bare goroutine with no retry,
// so a lost call leaves a draft row with no sort_order and no confirmation
// token. One cron pass must produce exactly what the callback would have.
func TestFinalizeStuckRegistrationsConvergesDroppedTrigger(t *testing.T) {
	env := newJobsTestEnv()
	stuck := testMentor("m1")
	env.repo.mentors["m1"] = stuck
	env.repo.stuckDraftMentors = []JobMentor{{ID: "m1", Name: stuck.Name, Email: stuck.Email}}

	summary, err := env.handlers.FinalizeStuckRegistrations(context.Background())
	if err != nil {
		t.Fatalf("FinalizeStuckRegistrations returned error: %v", err)
	}

	if summary.MentorsMatched != 1 || summary.MentorsFinalized != 1 || summary.EmailsSent != 1 {
		t.Errorf("summary = %+v, want 1 matched / 1 finalized / 1 sent", summary)
	}

	if len(env.repo.finalized) != 1 {
		t.Fatalf("finalized %d rows, want 1", len(env.repo.finalized))
	}
	update := env.repo.finalized[0]
	if update.MentorID != "m1" || update.Status != mentorStatusDraft {
		t.Errorf("update = %+v, want m1 still draft", update)
	}
	if update.SortOrder < 0 || update.SortOrder >= 1000 {
		t.Errorf("sort_order = %d, want a randomized 0..999 (the NULL is what locks the mentor out)", update.SortOrder)
	}
	if update.EmailConfirmationToken == nil || *update.EmailConfirmationToken == "" {
		t.Error("a confirmation token must be issued, otherwise the mentor still has no link")
	}

	if got := env.sender.templates(); len(got) != 1 || got[0] != "mentor-confirm-email" {
		t.Errorf("templates = %v, want [mentor-confirm-email]", got)
	}
}

// TestFinalizeStuckRegistrationsRetriesFailedConfirmationEmail: a send that
// fails (SES throttle, transient network) must not consume the replay — the
// mentor leaves the replay set only once they have actually been emailed.
func TestFinalizeStuckRegistrationsRetriesFailedConfirmationEmail(t *testing.T) {
	env := newJobsTestEnv()
	env.repo.mentors["m1"] = testMentor("m1")
	env.repo.stuckDraftMentors = []JobMentor{{ID: "m1", Name: "John Doe", Email: "john@example.com"}}
	env.sender.failTemplates["mentor-confirm-email"] = true

	summary, err := env.handlers.FinalizeStuckRegistrations(context.Background())
	if err != nil {
		t.Fatalf("FinalizeStuckRegistrations returned error: %v", err)
	}
	if summary.MentorsFinalized != 0 || summary.EmailFailures != 1 {
		t.Errorf("summary = %+v, want 0 finalized / 1 email failure", summary)
	}
	if len(env.repo.finalized) != 0 {
		t.Fatalf("finalized %d rows, want 0: sort_order and the token must stay NULL so the row is picked up again",
			len(env.repo.finalized))
	}

	// Next pass, SES healthy: the same row converges.
	delete(env.sender.failTemplates, "mentor-confirm-email")

	summary, err = env.handlers.FinalizeStuckRegistrations(context.Background())
	if err != nil {
		t.Fatalf("FinalizeStuckRegistrations returned error: %v", err)
	}
	if summary.MentorsFinalized != 1 || summary.EmailsSent != 1 {
		t.Errorf("summary = %+v, want 1 finalized / 1 sent on the retry", summary)
	}
	if len(env.repo.finalized) != 1 {
		t.Fatalf("finalized %d rows, want 1", len(env.repo.finalized))
	}
	if token := env.repo.finalized[0].EmailConfirmationToken; token == nil || *token == "" {
		t.Error("the retry must issue a confirmation token")
	}
}

func TestFinalizeStuckRegistrationsIsolatesFailures(t *testing.T) {
	env := newJobsTestEnv()
	env.repo.mentors["m2"] = testMentor("m2")
	// m1 has no mentor row (deleted between listing and replay).
	env.repo.stuckDraftMentors = []JobMentor{
		{ID: "m1", Name: "Ghost", Email: "ghost@example.com"},
		{ID: "m2", Name: "John Doe", Email: "john@example.com"},
	}

	summary, err := env.handlers.FinalizeStuckRegistrations(context.Background())
	if err != nil {
		t.Fatalf("FinalizeStuckRegistrations returned error: %v", err)
	}

	if summary.MentorsMatched != 2 || summary.MentorsFinalized != 1 {
		t.Errorf("summary = %+v, want 2 matched / 1 finalized", summary)
	}
	if len(env.repo.finalized) != 1 || env.repo.finalized[0].MentorID != "m2" {
		t.Errorf("finalized = %+v, want only m2", env.repo.finalized)
	}
}

func TestFinalizeStuckRegistrationsSkipsNonProduction(t *testing.T) {
	env := newJobsTestEnvWithConfig(func(cfg *config.Config) {
		cfg.Server.AppEnv = "development"
	})
	env.repo.stuckDraftMentors = []JobMentor{{ID: "m1", Name: "John", Email: "john@example.com"}}

	summary, err := env.handlers.FinalizeStuckRegistrations(context.Background())
	if err != nil {
		t.Fatalf("FinalizeStuckRegistrations returned error: %v", err)
	}
	if !summary.Skipped {
		t.Error("summary.Skipped = false, want true off production (the job sends email)")
	}
	if len(env.repo.finalized) != 0 {
		t.Errorf("finalized %d rows, want 0 when skipped", len(env.repo.finalized))
	}
}

func TestFinalizeStuckRegistrationsManualTrigger(t *testing.T) {
	env := newJobsTestEnv()
	env.repo.mentors["m1"] = testMentor("m1")
	env.repo.stuckDraftMentors = []JobMentor{{ID: "m1", Name: "John Doe", Email: "john@example.com"}}

	w := env.do(http.MethodPost, "/jobs/cron/finalize-stuck-registrations", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (route registered like the other cron jobs)", w.Code)
	}
	if len(env.repo.finalized) != 1 {
		t.Errorf("finalized %d rows, want 1", len(env.repo.finalized))
	}
}
