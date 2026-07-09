package worker

import (
	"context"
	"strings"
	"testing"

	"github.com/openmentor-io/openmentor-api/config"
	"github.com/openmentor-io/openmentor-api/pkg/analytics"
)

func TestSessionsWatcherSendsOneEmailPerMentor(t *testing.T) {
	env := newJobsTestEnv()
	env.repo.stalePendingMentors = []JobMentor{
		{ID: "m1", Name: "Alice Mentor", Email: "alice@example.com"},
		{ID: "m2", Name: "Bob Mentor", Email: "bob@example.com"},
	}
	env.repo.stalePendingRequests["m1"] = []JobReminderRequest{
		{ID: "r1", Name: "Mentee One", Description: "Help with Go & <testing>", Status: "pending", DaysAgo: 3},
		{ID: "r2", Name: "Mentee Two", Description: "", Status: "pending", DaysAgo: 1},
	}
	env.repo.stalePendingRequests["m2"] = []JobReminderRequest{
		{ID: "r3", Name: "Mentee Three", Description: "Career advice", Status: "pending", DaysAgo: 10},
	}

	summary, err := env.handlers.SessionsWatcher(context.Background())
	if err != nil {
		t.Fatalf("SessionsWatcher returned error: %v", err)
	}

	if summary.MentorsMatched != 2 || summary.EmailsSent != 2 || summary.EmailFailures != 0 {
		t.Errorf("summary = %+v, want 2 matched / 2 sent / 0 failed", summary)
	}
	if len(env.sender.attempts) != 2 {
		t.Fatalf("sent %d emails, want 2", len(env.sender.attempts))
	}

	first := env.sender.attempts[0]
	if first.TemplateName != "pending-requests-reminder" {
		t.Errorf("template = %q, want pending-requests-reminder", first.TemplateName)
	}
	if first.Recipient != "alice@example.com" {
		t.Errorf("recipient = %q, want alice@example.com", first.Recipient)
	}
	if got := first.Props["mentor_name"]; got != "Alice Mentor" {
		t.Errorf("mentor_name = %v, want Alice Mentor", got)
	}
	// pending_count is stringified like the func's String(requests.length).
	if got := first.Props["pending_count"]; got != "2" {
		t.Errorf("pending_count = %v (%T), want string \"2\"", got, got)
	}

	text, _ := first.Props["requests_list_text"].(string)
	wantText := "- Mentee One — waiting for 3 days\n  Help with Go & <testing>\n- Mentee Two — waiting for 1 day"
	if text != wantText {
		t.Errorf("requests_list_text = %q, want %q", text, wantText)
	}

	htmlList, _ := first.Props["requests_list"].(string)
	if !strings.HasPrefix(htmlList, `<ul style="padding-left: 20px; margin: 0;">`) {
		t.Errorf("requests_list missing <ul> wrapper: %q", htmlList)
	}
	if !strings.Contains(htmlList, "<strong>Mentee One — waiting for 3 days</strong>") {
		t.Errorf("requests_list missing first title: %q", htmlList)
	}
	if !strings.Contains(htmlList, "Help with Go &amp; &lt;testing&gt;") {
		t.Errorf("requests_list HTML not escaped: %q", htmlList)
	}
	// Empty description -> no <em> preview line for that item.
	if strings.Contains(htmlList, "<strong>Mentee Two — waiting for 1 day</strong><br>") {
		t.Errorf("empty preview should not add a <br><em> line: %q", htmlList)
	}

	// Analytics: one success event per mentor + the run_completed system event.
	if got := len(env.tracker.withOutcome("success")); got != 2 {
		t.Errorf("success events = %d, want 2", got)
	}
	completed := env.tracker.withOutcome("run_completed")
	if len(completed) != 1 {
		t.Fatalf("run_completed events = %d, want 1", len(completed))
	}
	if completed[0].event != analytics.EventMentorPendingRequestsReminded {
		t.Errorf("event = %q, want %q", completed[0].event, analytics.EventMentorPendingRequestsReminded)
	}
	if got := completed[0].props["mentors_reminded_count"]; got != 2 {
		t.Errorf("mentors_reminded_count = %v, want 2", got)
	}
}

func TestSessionsWatcherTruncatesLongDescriptions(t *testing.T) {
	env := newJobsTestEnv()
	longDescription := strings.Repeat("x", 200)
	env.repo.stalePendingMentors = []JobMentor{{ID: "m1", Name: "Alice", Email: "a@example.com"}}
	env.repo.stalePendingRequests["m1"] = []JobReminderRequest{
		{ID: "r1", Name: "Mentee", Description: longDescription, Status: "pending", DaysAgo: 2},
	}

	if _, err := env.handlers.SessionsWatcher(context.Background()); err != nil {
		t.Fatalf("SessionsWatcher returned error: %v", err)
	}

	text, _ := env.sender.attempts[0].Props["requests_list_text"].(string)
	want := "  " + strings.Repeat("x", 160) + "..."
	if !strings.Contains(text, want) {
		t.Errorf("preview not truncated to 160 chars + ellipsis: %q", text)
	}
	if strings.Contains(text, longDescription) {
		t.Error("full 200-char description should not appear in the email")
	}
}

func TestSessionsWatcherIsolatesSendFailures(t *testing.T) {
	env := newJobsTestEnv()
	env.repo.stalePendingMentors = []JobMentor{
		{ID: "m1", Name: "Alice", Email: "alice@example.com"},
		{ID: "m2", Name: "Bob", Email: "bob@example.com"},
	}
	env.repo.stalePendingRequests["m1"] = []JobReminderRequest{{ID: "r1", Name: "One", DaysAgo: 2}}
	env.repo.stalePendingRequests["m2"] = []JobReminderRequest{{ID: "r2", Name: "Two", DaysAgo: 2}}
	env.sender.failRecipients = map[string]bool{"alice@example.com": true}

	summary, err := env.handlers.SessionsWatcher(context.Background())
	if err != nil {
		t.Fatalf("a send failure must not fail the run: %v", err)
	}

	// Both sends attempted: the first failure does not stop the loop.
	if len(env.sender.attempts) != 2 {
		t.Fatalf("attempted %d sends, want 2", len(env.sender.attempts))
	}
	if summary.EmailsSent != 1 || summary.EmailFailures != 1 {
		t.Errorf("summary = %+v, want 1 sent / 1 failed", summary)
	}

	failures := env.tracker.withOutcome("error")
	if len(failures) != 1 {
		t.Fatalf("error events = %d, want 1", len(failures))
	}
	if failures[0].props["mentor_id"] != "m1" || failures[0].props["error_type"] != "email_send_failed" {
		t.Errorf("unexpected error event props: %+v", failures[0].props)
	}
	if got := len(env.tracker.withOutcome("run_completed")); got != 1 {
		t.Errorf("run_completed events = %d, want 1 (run must finish)", got)
	}
}

func TestSessionsWatcherSkipsMentorsWithoutRequests(t *testing.T) {
	env := newJobsTestEnv()
	env.repo.stalePendingMentors = []JobMentor{{ID: "m1", Name: "Alice", Email: "a@example.com"}}
	// No requests for m1 by the time the per-mentor query runs.

	summary, err := env.handlers.SessionsWatcher(context.Background())
	if err != nil {
		t.Fatalf("SessionsWatcher returned error: %v", err)
	}
	if len(env.sender.attempts) != 0 {
		t.Errorf("sent %d emails, want 0", len(env.sender.attempts))
	}
	// The func still counted the matched mentor in run_completed.
	if summary.MentorsMatched != 1 || summary.EmailsSent != 0 {
		t.Errorf("summary = %+v, want 1 matched / 0 sent", summary)
	}
}

func TestSessionsWatcherGateSkipsInNonProduction(t *testing.T) {
	env := newJobsTestEnvWithConfig(func(cfg *config.Config) {
		cfg.Server.AppEnv = "development"
	})
	env.repo.stalePendingMentors = []JobMentor{{ID: "m1", Name: "Alice", Email: "a@example.com"}}

	summary, err := env.handlers.SessionsWatcher(context.Background())
	if err != nil {
		t.Fatalf("SessionsWatcher returned error: %v", err)
	}
	if !summary.Skipped {
		t.Error("job must be skipped in non-production without DEV_EMAIL_OVERRIDE")
	}
	if len(env.sender.attempts) != 0 {
		t.Errorf("sent %d emails while gated, want 0", len(env.sender.attempts))
	}

	skips := env.tracker.withOutcome("skipped_non_production")
	if len(skips) != 1 || skips[0].event != analytics.EventMentorPendingRequestsReminded {
		t.Errorf("expected one skipped_non_production event, got %+v", skips)
	}
}

func TestSessionsWatcherGateUnlockedByDevEmailOverride(t *testing.T) {
	env := newJobsTestEnvWithConfig(func(cfg *config.Config) {
		cfg.Server.AppEnv = "development"
		cfg.Email.DevEmailOverride = "dev@example.com"
	})
	env.repo.stalePendingMentors = []JobMentor{{ID: "m1", Name: "Alice", Email: "a@example.com"}}
	env.repo.stalePendingRequests["m1"] = []JobReminderRequest{{ID: "r1", Name: "One", DaysAgo: 2}}

	summary, err := env.handlers.SessionsWatcher(context.Background())
	if err != nil {
		t.Fatalf("SessionsWatcher returned error: %v", err)
	}
	if summary.Skipped || summary.EmailsSent != 1 {
		t.Errorf("DEV_EMAIL_OVERRIDE should unlock the job: %+v", summary)
	}
}

func TestSessionsWatcherDBErrorAbortsRun(t *testing.T) {
	env := newJobsTestEnv()
	env.repo.listStaleMentorsErr = errDBDown

	_, err := env.handlers.SessionsWatcher(context.Background())
	if err == nil {
		t.Fatal("a DB failure must abort the run with an error")
	}

	errs := env.tracker.withOutcome("error")
	if len(errs) != 1 || errs[0].props["error_type"] != "db_error" {
		t.Errorf("expected one db_error event, got %+v", errs)
	}
}
