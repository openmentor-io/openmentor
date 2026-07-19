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
	"strings"

	"go.uber.org/zap"

	"github.com/openmentor-io/openmentor/api/config"
	"github.com/openmentor-io/openmentor/api/pkg/analytics"
	"github.com/openmentor-io/openmentor/api/pkg/email"
	"github.com/openmentor-io/openmentor/api/pkg/logger"
	"github.com/openmentor-io/openmentor/api/pkg/metrics"
)

// EmailSender is the subset of pkg/email.Sender the job handlers use;
// tests substitute a fake.
type EmailSender interface {
	Send(ctx context.Context, msg email.Message) (string, error)
}

// SlackInviter is the subset of pkg/slack.Inviter the job handlers use;
// nil means the community-Slack auto-invite is disabled. Tests substitute
// a fake.
type SlackInviter interface {
	InviteByEmail(ctx context.Context, email string) error
}

// Handlers holds the dependencies of the ported job handlers.
type Handlers struct {
	repo            JobsRepository
	email           EmailSender
	tracker         analytics.Tracker
	slack           SlackInviter
	moderatorsEmail string
	baseURL         string

	// Cron job settings (stage 3, timer-triggered functions).
	appEnv             string   // APP_ENV: drives the non-production gates
	devEmailOverride   string   // DEV_EMAIL_OVERRIDE: unlocks the email jobs off production
	highlightedMentors []string // HIGHLIGHTED_MENTORS ids pinned by randomize-sort-order
}

// NewHandlers wires the job handlers' dependencies. slackInviter may be nil
// (Slack auto-invite disabled).
func NewHandlers(repo JobsRepository, sender EmailSender, tracker analytics.Tracker, slackInviter SlackInviter, cfg *config.Config) *Handlers {
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
		slack:           slackInviter,
		moderatorsEmail: moderatorsEmail,
		baseURL:         baseURL,

		appEnv:             cfg.Server.AppEnv,
		devEmailOverride:   cfg.Email.DevEmailOverride,
		highlightedMentors: parseHighlightedMentors(cfg.Worker.HighlightedMentors),
	}
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
			zap.Error(err),
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
