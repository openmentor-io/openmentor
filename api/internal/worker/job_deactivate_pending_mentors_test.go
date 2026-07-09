package worker

import (
	"context"
	"testing"

	"github.com/openmentor-io/openmentor-api/config"
)

func TestDeactivatePendingMentorsDeactivatesAndNotifies(t *testing.T) {
	env := newJobsTestEnv()
	env.repo.mentorsToDeactivate = []JobMentor{
		{ID: "m1", Name: "Alice Mentor", Email: "alice@example.com"},
		{ID: "m2", Name: "Bob Mentor", Email: "bob@example.com"},
	}

	summary, err := env.handlers.DeactivatePendingMentors(context.Background())
	if err != nil {
		t.Fatalf("DeactivatePendingMentors returned error: %v", err)
	}

	if len(env.repo.deactivated) != 2 || env.repo.deactivated[0] != "m1" || env.repo.deactivated[1] != "m2" {
		t.Errorf("deactivated = %v, want [m1 m2]", env.repo.deactivated)
	}
	if summary.MentorsMatched != 2 || summary.MentorsDeactivated != 2 || summary.EmailsSent != 2 {
		t.Errorf("summary = %+v, want 2 matched / 2 deactivated / 2 sent", summary)
	}

	if len(env.sender.attempts) != 2 {
		t.Fatalf("sent %d emails, want 2", len(env.sender.attempts))
	}
	msg := env.sender.attempts[0]
	if msg.TemplateName != "profile-deactivated" {
		t.Errorf("template = %q, want profile-deactivated", msg.TemplateName)
	}
	if msg.Recipient != "alice@example.com" {
		t.Errorf("recipient = %q, want alice@example.com", msg.Recipient)
	}
	// The func's ProfileDeactivatedMessage carries only mentor_name; the
	// reactivation steps live in the template body.
	if got := msg.Props["mentor_name"]; got != "Alice Mentor" {
		t.Errorf("mentor_name = %v, want Alice Mentor", got)
	}
	if len(msg.Props) != 1 {
		t.Errorf("props = %v, want only mentor_name", msg.Props)
	}

	// The func tracked no analytics for this job.
	if env.tracker.count() != 0 {
		t.Errorf("tracked %d analytics events, want 0 (func parity)", env.tracker.count())
	}
}

func TestDeactivatePendingMentorsIsolatesEmailFailures(t *testing.T) {
	env := newJobsTestEnv()
	env.repo.mentorsToDeactivate = []JobMentor{
		{ID: "m1", Name: "Alice", Email: "alice@example.com"},
		{ID: "m2", Name: "Bob", Email: "bob@example.com"},
	}
	env.sender.failRecipients = map[string]bool{"alice@example.com": true}

	summary, err := env.handlers.DeactivatePendingMentors(context.Background())
	if err != nil {
		t.Fatalf("an email failure must not fail the run: %v", err)
	}

	// Both mentors deactivated despite the first email failing; both sends
	// attempted.
	if len(env.repo.deactivated) != 2 {
		t.Errorf("deactivated = %v, want both mentors", env.repo.deactivated)
	}
	if len(env.sender.attempts) != 2 {
		t.Errorf("attempted %d sends, want 2", len(env.sender.attempts))
	}
	if summary.MentorsDeactivated != 2 || summary.EmailsSent != 1 || summary.EmailFailures != 1 {
		t.Errorf("summary = %+v, want 2 deactivated / 1 sent / 1 failed", summary)
	}
}

func TestDeactivatePendingMentorsStatusWriteErrorAbortsRun(t *testing.T) {
	env := newJobsTestEnv()
	env.repo.mentorsToDeactivate = []JobMentor{{ID: "m1", Name: "Alice", Email: "a@example.com"}}
	env.repo.deactivateErr = errDBDown

	// The func ran the UPDATE outside the per-mentor try/catch: a DB
	// failure aborts the run.
	_, err := env.handlers.DeactivatePendingMentors(context.Background())
	if err == nil {
		t.Fatal("a status write failure must abort the run with an error")
	}
	if len(env.sender.attempts) != 0 {
		t.Errorf("no email should be sent for a mentor whose deactivation failed, got %d", len(env.sender.attempts))
	}
}

func TestDeactivatePendingMentorsGateSkipsInNonProduction(t *testing.T) {
	env := newJobsTestEnvWithConfig(func(cfg *config.Config) {
		cfg.Server.AppEnv = "development"
	})
	env.repo.mentorsToDeactivate = []JobMentor{{ID: "m1", Name: "Alice", Email: "a@example.com"}}

	summary, err := env.handlers.DeactivatePendingMentors(context.Background())
	if err != nil {
		t.Fatalf("DeactivatePendingMentors returned error: %v", err)
	}
	if !summary.Skipped {
		t.Error("job must be skipped in non-production without DEV_EMAIL_OVERRIDE")
	}
	if len(env.repo.deactivated) != 0 || len(env.sender.attempts) != 0 {
		t.Error("gated run must not deactivate or email anyone")
	}
	// Unlike the reminder jobs, the func returned silently here.
	if env.tracker.count() != 0 {
		t.Errorf("tracked %d analytics events on skip, want 0 (func parity)", env.tracker.count())
	}
}

func TestDeactivatePendingMentorsGateUnlockedByDevEmailOverride(t *testing.T) {
	env := newJobsTestEnvWithConfig(func(cfg *config.Config) {
		cfg.Server.AppEnv = "development"
		cfg.Email.DevEmailOverride = "dev@example.com"
	})
	env.repo.mentorsToDeactivate = []JobMentor{{ID: "m1", Name: "Alice", Email: "a@example.com"}}

	summary, err := env.handlers.DeactivatePendingMentors(context.Background())
	if err != nil {
		t.Fatalf("DeactivatePendingMentors returned error: %v", err)
	}
	if summary.Skipped || summary.MentorsDeactivated != 1 {
		t.Errorf("DEV_EMAIL_OVERRIDE should unlock the job: %+v", summary)
	}
}
