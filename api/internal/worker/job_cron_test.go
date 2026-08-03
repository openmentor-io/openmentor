package worker

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/openmentor-io/openmentor/api/config"
)

// TestCronTriggerEndpointsRegistered verifies every cron job is manually
// triggerable via POST /jobs/cron/<name> (and only via POST).
func TestCronTriggerEndpointsRegistered(t *testing.T) {
	env := newJobsTestEnv()

	names := []string{
		"sessions-watcher",
		"update-status-reminder",
		"deactivate-pending-mentors",
		"randomize-sort-order",
	}
	for _, name := range names {
		w := env.do(http.MethodPost, "/jobs/cron/"+name, nil)
		if w.Code != http.StatusOK {
			t.Errorf("POST /jobs/cron/%s = %d, want 200", name, w.Code)
		}

		var summary JobSummary
		if err := json.Unmarshal(w.Body.Bytes(), &summary); err != nil {
			t.Fatalf("POST /jobs/cron/%s returned invalid JSON: %v", name, err)
		}
		if summary.Job != name {
			t.Errorf("summary.Job = %q, want %q", summary.Job, name)
		}

		if w := env.do(http.MethodGet, "/jobs/cron/"+name, nil); w.Code != http.StatusNotFound {
			t.Errorf("GET /jobs/cron/%s = %d, want 404 (POST only)", name, w.Code)
		}
	}
}

func TestCronTriggerReturnsRunSummary(t *testing.T) {
	env := newJobsTestEnv()
	env.repo.stalePendingMentors = []JobMentor{{ID: "m1", Name: "Alice", Email: "a@example.com"}}
	env.repo.stalePendingRequests["m1"] = []JobReminderRequest{{ID: "r1", Name: "One", DaysAgo: 2}}

	w := env.do(http.MethodPost, "/jobs/cron/sessions-watcher", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var summary JobSummary
	if err := json.Unmarshal(w.Body.Bytes(), &summary); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if summary.MentorsMatched != 1 || summary.EmailsSent != 1 || summary.EmailFailures != 0 {
		t.Errorf("summary = %+v, want 1 matched / 1 sent / 0 failed", summary)
	}
	if len(env.sender.attempts) != 1 {
		t.Errorf("trigger sent %d emails, want 1", len(env.sender.attempts))
	}
}

func TestCronTriggerReportsGateSkip(t *testing.T) {
	env := newJobsTestEnvWithConfig(func(cfg *config.Config) {
		cfg.Server.AppEnv = "staging"
	})

	w := env.do(http.MethodPost, "/jobs/cron/deactivate-pending-mentors", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var summary JobSummary
	if err := json.Unmarshal(w.Body.Bytes(), &summary); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if !summary.Skipped {
		t.Errorf("summary = %+v, want skipped=true in non-production", summary)
	}
}

func TestCronTriggerReturns500OnJobError(t *testing.T) {
	env := newJobsTestEnv()
	env.repo.listStaleMentorsErr = errDBDown

	w := env.do(http.MethodPost, "/jobs/cron/sessions-watcher", nil)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}

	var body struct {
		Summary JobSummary `json:"summary"`
		Error   string     `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body.Error == "" {
		t.Error("error message missing from 500 body")
	}
	if body.Summary.Job != "sessions-watcher" {
		t.Errorf("partial summary missing: %+v", body.Summary)
	}
}

// TestCronTriggersRequireWorkerToken verifies the manual triggers sit
// behind the same X-Worker-Token middleware as the rest of /jobs.
func TestCronTriggersRequireWorkerToken(t *testing.T) {
	cfg := testConfig()
	cfg.Worker.AuthToken = "secret-token"
	handlers := NewHandlers(newFakeRepo(), &fakeEmailSender{}, &recordingTracker{}, cfg)
	s := NewServer(cfg, nil)
	s.RegisterCronRoutes(handlers)

	w := performRequest(s.Engine(), http.MethodPost, "/jobs/cron/randomize-sort-order", nil)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("missing token: status = %d, want 401", w.Code)
	}

	w = performRequest(s.Engine(), http.MethodPost, "/jobs/cron/randomize-sort-order",
		map[string]string{WorkerTokenHeader: "wrong"})
	if w.Code != http.StatusUnauthorized {
		t.Errorf("wrong token: status = %d, want 401", w.Code)
	}

	w = performRequest(s.Engine(), http.MethodPost, "/jobs/cron/randomize-sort-order",
		map[string]string{WorkerTokenHeader: "secret-token"})
	if w.Code != http.StatusOK {
		t.Errorf("correct token: status = %d, want 200", w.Code)
	}
}

// TestFragmentBytesArePinned pins the exact markup of the two server-built
// props against an input carrying every character class that escaping treats
// differently. The pre-change code concatenated html.EscapeString output; the
// bytes below are that output except for the two characters where
// html/template's HTML-text escaper is wider — '+' (&#43;) and a NUL byte
// (U+FFFD, unreachable from Postgres text). Mail clients render both back, so
// nothing is user-visible; the point is that the delta is exactly this, and
// stays this.
func TestFragmentBytesArePinned(t *testing.T) {
	const input = `C++ & "Tom's" <b>plan</b> 5 < 10`

	t.Run("decline_info", func(t *testing.T) {
		got := string(buildDeclineInfoHTML("", input))
		want := `<br><br>` +
			`<div style="font-family: inherit; text-align: inherit"><strong>Comment:</strong> ` +
			`C&#43;&#43; &amp; &#34;Tom&#39;s&#34; &lt;b&gt;plan&lt;/b&gt; 5 &lt; 10</div>`
		if got != want {
			t.Errorf("decline_info fragment\n got: %s\nwant: %s", got, want)
		}
	})

	t.Run("requests_list", func(t *testing.T) {
		got := string(renderFragment(staleRequestsListTpl, []string{input}))
		want := `<ul style="padding-left: 20px; margin: 0;">` +
			`<li style="margin-bottom: 8px;">` +
			`C&#43;&#43; &amp; &#34;Tom&#39;s&#34; &lt;b&gt;plan&lt;/b&gt; 5 &lt; 10</li>` +
			`</ul>`
		if got != want {
			t.Errorf("requests_list fragment\n got: %s\nwant: %s", got, want)
		}
	})

	t.Run("pending requests_list", func(t *testing.T) {
		got := string(renderFragment(pendingRequestsListTpl,
			[]pendingRequestItem{{Title: input, Preview: "5 < 10"}}))
		want := `<ul style="padding-left: 20px; margin: 0;">` +
			`<li style="margin-bottom: 8px;"><strong>` +
			`C&#43;&#43; &amp; &#34;Tom&#39;s&#34; &lt;b&gt;plan&lt;/b&gt; 5 &lt; 10</strong>` +
			`<br><em>5 &lt; 10</em></li>` +
			`</ul>`
		if got != want {
			t.Errorf("pending requests_list fragment\n got: %s\nwant: %s", got, want)
		}
	})
}
