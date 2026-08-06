// Job handlers ported from the openmentor-func Azure Functions app (stage 2
// of the Go worker build). Each handler mirrors the corresponding
// openmentor-func/<name>/index.ts business logic: DB reads/writes, English
// emails through pkg/email and analytics events through pkg/analytics.
// Telegram notifications from the legacy func app were intentionally NOT
// ported (English-market worker sends email only).
package worker

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"go.uber.org/zap"

	"github.com/openmentor-io/openmentor/api/config"
	"github.com/openmentor-io/openmentor/api/pkg/analytics"
	"github.com/openmentor-io/openmentor/api/pkg/email"
	"github.com/openmentor-io/openmentor/api/pkg/logger"
	"github.com/openmentor-io/openmentor/api/pkg/metrics"
)

// Moderation actions carried on the mentor-moderation trigger payload.
const (
	moderationActionApprove = "approve"
	moderationActionDecline = "decline"
	moderationActionReturn  = "return"
)

// Mentor lifecycle statuses the jobs read and write (mirrors the API's
// mentors.status values).
const (
	mentorStatusActive   = "active"
	mentorStatusDraft    = "draft"
	mentorStatusDeclined = "declined"
)

// Failure kinds reported on job results and as the analytics `error_type`
// property. Tests keep the literals on purpose: they pin the wire values.
const (
	errTypeDBError         = "db_error"
	errTypeEmailSendFailed = "email_send_failed"
)

// EmailSender is the subset of pkg/email.Sender the job handlers use;
// tests substitute a fake.
type EmailSender interface {
	Send(ctx context.Context, msg email.Message) (string, error)
}

// Handlers holds the dependencies of the ported job handlers.
type Handlers struct {
	repo            JobsRepository
	email           EmailSender
	tracker         analytics.Tracker
	moderatorsEmail string
	baseURL         string

	// Cron job settings (stage 3, timer-triggered functions).
	appEnv             string   // APP_ENV: drives the non-production gates
	devEmailOverride   string   // DEV_EMAIL_OVERRIDE: unlocks the email jobs off production
	highlightedMentors []string // HIGHLIGHTED_MENTORS ids pinned by randomize-sort-order

	// Profile purge (D70): WORKER_PROFILE_PURGE_CRON schedules the job,
	// WORKER_PROFILE_PURGE_RETENTION_DAYS decides how long a deleted profile
	// survives before it is erased. Both validated at boot
	// (config.ValidateForWorker) — a zero retention here would mean "erase
	// everything deleted", so it must never arrive as an unparsed default.
	profilePurgeCron          string
	profilePurgeRetentionDays int

	// DISCORD_MENTORS_PRIVATE_INVITE_LINK: private mentors' Discord invite,
	// shown in the approval welcome email. Required — the template renders the
	// section unconditionally (SES templates don't support {{#if}}), so an
	// empty value ships a link with an empty href.
	discordInviteLink string
}

// NewHandlers wires the job handlers' dependencies.
func NewHandlers(repo JobsRepository, sender EmailSender, tracker analytics.Tracker, cfg *config.Config) *Handlers {
	if tracker == nil {
		tracker = analytics.NoopTracker{}
	}

	moderatorsEmail := strings.TrimSpace(cfg.Email.ModeratorsEmail)
	if moderatorsEmail == "" {
		moderatorsEmail = email.DefaultModeratorsEmail
	}

	baseURL := strings.TrimRight(strings.TrimSpace(cfg.Server.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://openmentor.io"
	}

	return &Handlers{
		repo:            repo,
		email:           sender,
		tracker:         tracker,
		moderatorsEmail: moderatorsEmail,
		baseURL:         baseURL,

		appEnv:             cfg.Server.AppEnv,
		devEmailOverride:   cfg.Email.DevEmailOverride,
		highlightedMentors: parseHighlightedMentors(cfg.Worker.HighlightedMentors),

		profilePurgeCron:          resolvePurgeCron(cfg.Worker.ProfilePurgeCron),
		profilePurgeRetentionDays: resolvePurgeRetentionDays(cfg.Worker.ProfilePurgeRetentionDays),

		discordInviteLink: strings.TrimSpace(cfg.Email.DiscordInviteLink),
	}
}

// Profile purge fallbacks (D70). These mirror the viper defaults in
// config.setDefaults — keep the two in step — and exist because a purge setting
// must never be able to break the worker or, worse, be read as "erase
// everything now":
//
//   - An unset value reaches here from any Config built without viper, which is
//     every test that constructs one by hand.
//   - A typo'd number (WORKER_PROFILE_PURGE_RETENTION_DAYS=thirty) parses to 0,
//     and 0 days past deletion is EVERY deleted profile.
//   - An empty schedule makes cron.AddJob fail, which fails NewCron, which
//     stops the worker booting — taking every transactional email with it.
//
// So both fall back rather than propagate, loudly enough to find in the log.
const (
	defaultProfilePurgeCron          = "0 15 3 * * *"
	defaultProfilePurgeRetentionDays = 30
)

func resolvePurgeCron(configured string) string {
	if strings.TrimSpace(configured) != "" {
		return configured
	}
	logger.Warn("WORKER_PROFILE_PURGE_CRON is empty; falling back to the default schedule",
		zap.String("schedule", defaultProfilePurgeCron))
	return defaultProfilePurgeCron
}

func resolvePurgeRetentionDays(configured int) int {
	if configured > 0 {
		return configured
	}
	logger.Warn("WORKER_PROFILE_PURGE_RETENTION_DAYS is unset or not a positive number; falling back to the default",
		zap.Int("configured", configured),
		zap.Int("retention_days", defaultProfilePurgeRetentionDays))
	return defaultProfilePurgeRetentionDays
}

// parseHighlightedMentors mirrors randomize-sort-order/index.ts verbatim:
// process.env.HIGHLIGHTED_MENTORS ? HIGHLIGHTED_MENTORS.split(',') : [] -
// a bare comma split with NO trimming, so entries must be exact mentor ids.
func parseHighlightedMentors(raw string) []string {
	if raw == "" {
		return nil
	}
	return strings.Split(raw, ",")
}

// RegisterJobRoutes registers the ported event handlers under /jobs,
// with the same methods the stage-1 stubs advertised, plus the
// draft-status workflow jobs (mentor-confirmed, mentor-confirm-email).
func (s *Server) RegisterJobRoutes(h *Handlers) {
	s.RegisterHandler("/new-mentor-watcher", h.NewMentorWatcher, "POST", "GET")
	s.RegisterHandler("/mentor-confirmed", h.MentorConfirmed, "POST", "GET")
	s.RegisterHandler("/mentor-confirm-email", h.MentorConfirmEmail, "POST", "GET")
	s.RegisterHandler("/new-request-watcher", h.NewRequestWatcher, "POST", "GET")
	s.RegisterHandler("/mentor-login-email", h.MentorLoginEmail)
	s.RegisterHandler("/moderator-login-email", h.ModeratorLoginEmail)
	s.RegisterHandler("/mentor-moderation-action", h.MentorModerationAction)
	s.RegisterHandler("/process-mentee-review", h.ProcessMenteeReview, "POST", "GET")
	s.RegisterHandler("/request-process-finished", h.RequestProcessFinished, "GET")
	s.RegisterHandler("/profile-migrated", h.ProfileMigrated)
}

// sendEmail sends one message, recording metrics and logging failures.
func (h *Handlers) sendEmail(ctx context.Context, job string, msg email.Message) error {
	if _, err := h.email.Send(ctx, msg); err != nil {
		metrics.WorkerEmailSendsTotal.WithLabelValues(msg.TemplateName, "error").Inc()
		logger.Error("Job email send failed",
			zap.String("job", job),
			zap.String("template", msg.TemplateName),
			zap.String("recipient", msg.Recipient),
			logger.RedactedError(err),
		)
		return err
	}
	metrics.WorkerEmailSendsTotal.WithLabelValues(msg.TemplateName, "success").Inc()
	return nil
}

// sendEmails attempts EVERY message even when earlier sends fail, then
// returns the joined error. This mirrors the func app's Promise.all([...])
// semantics where all sends were started regardless of individual failures.
func (h *Handlers) sendEmails(ctx context.Context, job string, msgs ...email.Message) error {
	var errs []error
	for _, msg := range msgs {
		if err := h.sendEmail(ctx, job, msg); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// track is a small wrapper for the handler analytics events.
func (h *Handlers) track(ctx context.Context, event, distinctID string, props map[string]interface{}) {
	h.tracker.Track(ctx, event, distinctID, props)
}

// trimMentorName mirrors the func app: name.trim().replace('  ', ' ')
// (first double-space only, as in JS String.replace with a string pattern).
func trimMentorName(value string) string {
	return strings.Replace(strings.TrimSpace(value), "  ", " ", 1)
}

// valueOrDash mirrors the func app's Mentor class fallbacks
// (job = record.get("JobTitle") || '-', workplace || '-').
func valueOrDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

// mentorProfileURL mirrors Mentor.getProfileUrl():
// https://openmentor.io/mentor/{slug} (empty when the mentor has no slug).
func (h *Handlers) mentorProfileURL(slug string) string {
	if slug == "" {
		return ""
	}
	return h.baseURL + "/mentor/" + slug
}

// mentorRequestURL is the mentor's own view of one request in their inbox:
// https://openmentor.io/mentor/requests/{requestID}. Unauthenticated visitors
// are bounced to /mentor/login, so the deep link is safe to email.
func (h *Handlers) mentorRequestURL(requestID string) string {
	if requestID == "" {
		return h.baseURL + "/mentor"
	}
	return h.baseURL + "/mentor/requests/" + url.PathEscape(requestID)
}

// adminRequestURL is the moderation portal's view of one request:
// https://openmentor.io/admin/mentors/{mentorID}/requests/{requestID}. That
// page is admin-only — moderator-role accounts are redirected to the pending
// queue — so the moderator email pairs it with adminPortalURL.
func (h *Handlers) adminRequestURL(mentorID, requestID string) string {
	if mentorID == "" || requestID == "" {
		return h.adminPortalURL()
	}
	return h.baseURL + "/admin/mentors/" + url.PathEscape(mentorID) + "/requests/" + url.PathEscape(requestID)
}

// adminPortalURL is the moderation portal entry point.
func (h *Handlers) adminPortalURL() string {
	return h.baseURL + "/admin"
}

// mentorProfileShareURL builds the LinkedIn share-dialog link for a mentor's
// profile, with share attribution (see docs/analytics-utm.md). Used by the
// approval email's "Share your profile" CTA — the profile link unfurls into
// the branded OG card. Empty when the mentor has no slug.
func (h *Handlers) mentorProfileShareURL(slug string) string {
	profile := h.mentorProfileURL(slug)
	if profile == "" {
		return ""
	}
	shared := profile + "?utm_source=mentor-share&utm_medium=social&utm_campaign=profile-share"
	return "https://www.linkedin.com/sharing/share-offsite/?url=" + url.QueryEscape(shared)
}
