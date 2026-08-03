// Cron job plumbing shared by the four timer jobs ported from
// openmentor-func (stage 3): the per-run summary, the job function type,
// the production gate and the email list-building helpers the reminder
// messages share.
package worker

import (
	"context"
	"fmt"
	"html/template"
	"strings"
)

// JobSummary is the per-run result of a cron job: what matched, what was
// sent and what failed. It is logged after every scheduled run and returned
// as JSON by the manual POST /jobs/cron/<name> triggers.
type JobSummary struct {
	Job string `json:"job"`

	// Skipped reports the func app's non-production gate: outside
	// production the email jobs only run when DEV_EMAIL_OVERRIDE reroutes
	// emails to a dev inbox.
	Skipped bool `json:"skipped,omitempty"`

	MentorsMatched int `json:"mentors_matched"`
	EmailsSent     int `json:"emails_sent"`
	EmailFailures  int `json:"email_failures"`

	// deactivate-pending-mentors only.
	MentorsDeactivated int `json:"mentors_deactivated,omitempty"`

	// finalize-stuck-registrations only.
	MentorsFinalized int `json:"mentors_finalized,omitempty"`

	// randomize-sort-order only.
	SortOrdersRandomized int `json:"sort_orders_randomized,omitempty"`
	HighlightedPinned    int `json:"highlighted_pinned,omitempty"`
	// WritesSkipped mirrors the func's randomize gate: the job runs
	// everywhere but only writes sort orders in production.
	WritesSkipped bool `json:"writes_skipped,omitempty"`
}

// CronJobFunc is a single cron job run, ported from a timer-triggered
// function in openmentor-func.
type CronJobFunc func(ctx context.Context) (JobSummary, error)

// skipNonProduction is the func app's timer gate, verbatim:
// process.env.APP_ENV !== 'production' && !process.env.DEV_EMAIL_OVERRIDE.
// In non-production the email jobs only run when DEV_EMAIL_OVERRIDE routes
// all emails to a dev inbox. randomize-sort-order deliberately does NOT use
// this gate (see its handler).
func (h *Handlers) skipNonProduction() bool {
	return h.appEnv != "production" && h.devEmailOverride == ""
}

// requestsListOpen / requestsListClose are the exact <ul> markup the func's
// reminder message classes wrapped their <li> items in.
const (
	requestsListOpen  = `<ul style="padding-left: 20px; margin: 0;">`
	requestsListClose = `</ul>`
)

// renderFragment builds one of the server-generated markup fragments the
// email templates interpolate as-is (requests_list, decline_info). The
// fragment is assembled by html/template rather than by concatenating
// pre-escaped strings, so the mentee-supplied text inside it is escaped
// structurally — which is what makes returning template.HTML, and therefore
// skipping escaping in the outer template, correct.
func renderFragment(tpl *template.Template, data any) template.HTML {
	var b strings.Builder
	if err := tpl.Execute(&b, data); err != nil {
		// Unreachable: the fragment templates are compile-time constants and
		// strings.Builder never fails to write.
		panic(fmt.Sprintf("email fragment %s: %v", tpl.Name(), err))
	}
	return template.HTML(b.String())
}

// daysWording mirrors the func's staleness wording:
// days === 1 ? '1 day' : `${days} days`.
func daysWording(days int) string {
	if days == 1 {
		return "1 day"
	}
	return fmt.Sprintf("%d days", days)
}
