package worker

import (
	"context"
	"strings"
	"testing"

	"github.com/openmentor-io/openmentor-api/config"
	"github.com/openmentor-io/openmentor-api/pkg/analytics"
)

func TestUpdateStatusReminderSendsOneEmailPerMentor(t *testing.T) {
	env := newJobsTestEnv()
	env.repo.staleProgressMentors = []JobMentor{
		{ID: "m1", Name: "Alice Mentor", Email: "alice@example.com"},
	}
	env.repo.staleProgressRequests["m1"] = []JobReminderRequest{
		{ID: "r1", Name: "Mentee One", Status: "contacted", DaysAgo: 6},
		{ID: "r2", Name: "Mentee Two", Status: "working", DaysAgo: 1},
		{ID: "r3", Name: "Mentee Three", Status: "weird_status", DaysAgo: 8},
	}

	summary, err := env.handlers.UpdateStatusReminder(context.Background())
	if err != nil {
		t.Fatalf("UpdateStatusReminder returned error: %v", err)
	}
	if summary.MentorsMatched != 1 || summary.EmailsSent != 1 {
		t.Errorf("summary = %+v, want 1 matched / 1 sent", summary)
	}
	if len(env.sender.attempts) != 1 {
		t.Fatalf("sent %d emails, want 1", len(env.sender.attempts))
	}

	msg := env.sender.attempts[0]
	if msg.TemplateName != "status-update-reminder" {
		t.Errorf("template = %q, want status-update-reminder", msg.TemplateName)
	}
	if msg.Recipient != "alice@example.com" {
		t.Errorf("recipient = %q, want alice@example.com", msg.Recipient)
	}
	if got := msg.Props["mentor_name"]; got != "Alice Mentor" {
		t.Errorf("mentor_name = %v, want Alice Mentor", got)
	}

	// Status labels + staleness wording, mirroring the func's message class
	// (unknown statuses fall back to the raw value).
	text, _ := msg.Props["requests_list_text"].(string)
	wantText := "- Mentee One — \"Contacted\" for 6 days\n" +
		"- Mentee Two — \"In progress\" for 1 day\n" +
		"- Mentee Three — \"weird_status\" for 8 days"
	if text != wantText {
		t.Errorf("requests_list_text = %q, want %q", text, wantText)
	}

	htmlList, _ := msg.Props["requests_list"].(string)
	if !strings.HasPrefix(htmlList, `<ul style="padding-left: 20px; margin: 0;">`) {
		t.Errorf("requests_list missing <ul> wrapper: %q", htmlList)
	}
	if !strings.Contains(htmlList, `Mentee One — &#34;Contacted&#34; for 6 days`) {
		t.Errorf("requests_list missing escaped first line: %q", htmlList)
	}

	success := env.tracker.withOutcome("success")
	if len(success) != 1 || success[0].event != analytics.EventMentorStatusUpdateReminded {
		t.Fatalf("expected one success event, got %+v", success)
	}
	if success[0].props["stale_requests_count"] != 3 {
		t.Errorf("stale_requests_count = %v, want 3", success[0].props["stale_requests_count"])
	}
	completed := env.tracker.withOutcome("run_completed")
	if len(completed) != 1 || completed[0].props["mentors_reminded_count"] != 1 {
		t.Errorf("expected run_completed with mentors_reminded_count=1, got %+v", completed)
	}
}

func TestUpdateStatusReminderIsolatesSendFailures(t *testing.T) {
	env := newJobsTestEnv()
	env.repo.staleProgressMentors = []JobMentor{
		{ID: "m1", Name: "Alice", Email: "alice@example.com"},
		{ID: "m2", Name: "Bob", Email: "bob@example.com"},
	}
	env.repo.staleProgressRequests["m1"] = []JobReminderRequest{{ID: "r1", Name: "One", Status: "contacted", DaysAgo: 6}}
	env.repo.staleProgressRequests["m2"] = []JobReminderRequest{{ID: "r2", Name: "Two", Status: "working", DaysAgo: 6}}
	env.sender.failRecipients = map[string]bool{"alice@example.com": true}

	summary, err := env.handlers.UpdateStatusReminder(context.Background())
	if err != nil {
		t.Fatalf("a send failure must not fail the run: %v", err)
	}
	if len(env.sender.attempts) != 2 {
		t.Fatalf("attempted %d sends, want 2", len(env.sender.attempts))
	}
	if summary.EmailsSent != 1 || summary.EmailFailures != 1 {
		t.Errorf("summary = %+v, want 1 sent / 1 failed", summary)
	}

	failures := env.tracker.withOutcome("error")
	if len(failures) != 1 || failures[0].props["error_type"] != "email_send_failed" {
		t.Errorf("expected one email_send_failed event, got %+v", failures)
	}
}

func TestUpdateStatusReminderGateSkipsInNonProduction(t *testing.T) {
	env := newJobsTestEnvWithConfig(func(cfg *config.Config) {
		cfg.Server.AppEnv = "staging"
	})

	summary, err := env.handlers.UpdateStatusReminder(context.Background())
	if err != nil {
		t.Fatalf("UpdateStatusReminder returned error: %v", err)
	}
	if !summary.Skipped {
		t.Error("job must be skipped in non-production without DEV_EMAIL_OVERRIDE")
	}

	skips := env.tracker.withOutcome("skipped_non_production")
	if len(skips) != 1 || skips[0].event != analytics.EventMentorStatusUpdateReminded {
		t.Errorf("expected one skipped_non_production event, got %+v", skips)
	}
}

func TestUpdateStatusReminderDBErrorAbortsRun(t *testing.T) {
	env := newJobsTestEnv()
	env.repo.staleProgressMentors = []JobMentor{{ID: "m1", Name: "Alice", Email: "a@example.com"}}
	env.repo.listStaleRequestsErr = errDBDown

	_, err := env.handlers.UpdateStatusReminder(context.Background())
	if err == nil {
		t.Fatal("a DB failure must abort the run with an error")
	}
	errs := env.tracker.withOutcome("error")
	if len(errs) != 1 || errs[0].props["error_type"] != "db_error" {
		t.Errorf("expected one db_error event, got %+v", errs)
	}
}
