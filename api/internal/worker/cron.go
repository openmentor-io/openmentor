package worker

import (
	"context"
	"fmt"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/robfig/cron/v3"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.uber.org/zap"

	"github.com/openmentor-io/openmentor/api/pkg/errortracking"
	"github.com/openmentor-io/openmentor/api/pkg/logger"
	"github.com/openmentor-io/openmentor/api/pkg/metrics"
)

// Cron schedules the worker's recurring jobs (see Handlers.CronJobs for
// the job table and schedules).
type Cron struct {
	cron *cron.Cron
}

// CronJob couples a job with its schedule, for both the scheduler and the
// manual POST /jobs/cron/<name> trigger endpoints.
type CronJob struct {
	Name     string
	Schedule string
	Run      CronJobFunc
}

// CronJobs returns the four scheduled jobs, ported from the func app's
// timer-triggered functions. The schedules are the func app's NCRONTAB
// expressions verbatim (6 fields, seconds first), taken from each
// function.json timer definition:
//
//	sessions-watcher            "0 30 8 * * *"    daily 08:30
//	update-status-reminder      "0 0 10 * * Wed"  Wednesdays 10:00
//	deactivate-pending-mentors  "0 0 10 * * Wed"  Wednesdays 10:00
//	randomize-sort-order        "0 0 1 * * *"     daily 01:00
func (h *Handlers) CronJobs() []CronJob {
	return []CronJob{
		{Name: "sessions-watcher", Schedule: "0 30 8 * * *", Run: h.SessionsWatcher},
		{Name: "update-status-reminder", Schedule: "0 0 10 * * Wed", Run: h.UpdateStatusReminder},
		{Name: "deactivate-pending-mentors", Schedule: "0 0 10 * * Wed", Run: h.DeactivatePendingMentors},
		{Name: "randomize-sort-order", Schedule: "0 0 1 * * *", Run: h.RandomizeSortOrder},
	}
}

// NewCron builds the scheduler and registers the four ported jobs.
func NewCron(h *Handlers) (*Cron, error) {
	c := &Cron{
		// Azure NCRONTAB expressions include a seconds field, so use a
		// seconds-aware parser and keep the expressions verbatim.
		cron: cron.New(cron.WithParser(cron.NewParser(
			cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow,
		))),
	}

	for _, job := range h.CronJobs() {
		run := job.Run
		name := job.Name
		if _, err := c.cron.AddFunc(job.Schedule, func() {
			runCronJob(context.Background(), name, run) //nolint:errcheck // logged + counted inside
		}); err != nil {
			return nil, err
		}
		logger.Info("Registered cron job",
			zap.String("job", job.Name),
			zap.String("schedule", job.Schedule),
		)
	}

	return c, nil
}

// RegisterCronRoutes exposes every cron job as a manually-triggerable
// endpoint - POST /jobs/cron/<name> - that runs the same function once and
// returns its JobSummary as JSON (500 with the partial summary on error).
// Invaluable for staging smoke tests. The routes sit in the /jobs group,
// so the same X-Worker-Token middleware guards them.
func (s *Server) RegisterCronRoutes(h *Handlers) {
	for _, job := range h.CronJobs() {
		run := job.Run
		name := job.Name
		s.jobs.POST("/cron/"+name, func(c *gin.Context) {
			summary, err := runCronJob(c.Request.Context(), name, run)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"summary": summary,
					"error":   err.Error(),
				})
				return
			}
			c.JSON(http.StatusOK, summary)
		})
	}
}

// runCronJob executes one job run with panic recovery, duration/outcome
// metrics and per-run summary logging. It is shared by the scheduler and
// the manual POST /jobs/cron/<name> triggers.
//
// Production gating (stage-3 note): the func app's gate - run only when
// APP_ENV=production, unlocked elsewhere by DEV_EMAIL_OVERRIDE - now lives
// INSIDE each job handler (Handlers.skipNonProduction) instead of this
// wrapper, so the jobs can emit the same skipped_non_production analytics
// events the func did and manual triggers report the skip in their summary
// JSON. A skipped run is still counted with the "skipped" outcome, like
// stage 1's wrapper gate. randomize-sort-order deliberately never skips:
// per the func it always runs and only guards its DB writes on production,
// with no DEV_EMAIL_OVERRIDE unlock (see its handler).
func runCronJob(ctx context.Context, name string, job CronJobFunc) (summary JobSummary, err error) {
	log := logger.With(zap.String("job", name))
	start := time.Now()
	outcome := "success"

	// One span per run. Scheduler runs pass context.Background(), so this
	// is a trace root; manual POST /jobs/cron/<name> triggers pass the Gin
	// request context, so the span nests under the otelgin server span.
	ctx, span := otel.Tracer("internal/worker").Start(ctx, "cron."+name)

	defer func() {
		if recovered := recover(); recovered != nil {
			outcome = "panic"
			err = fmt.Errorf("cron job %s panicked: %v", name, recovered)
			stack := debug.Stack()
			log.Error("Cron job panicked",
				zap.Any("panic", recovered),
				zap.String("stack", string(stack)),
			)
			errortracking.CapturePanic(recovered, stack)
		}
		duration := time.Since(start)
		metrics.WorkerCronRunDuration.WithLabelValues(name).Observe(duration.Seconds())
		metrics.WorkerCronRunsTotal.WithLabelValues(name, outcome).Inc()
		span.SetAttributes(
			attribute.String("cron.job", name),
			attribute.String("cron.outcome", outcome),
			attribute.Float64("cron.duration_seconds", duration.Seconds()),
			attribute.Bool("cron.skipped", summary.Skipped),
		)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, outcome)
		}
		span.End()
		log.Info("Cron job finished",
			zap.String("outcome", outcome),
			zap.Duration("duration", duration),
			zap.Bool("job_skipped", summary.Skipped),
			zap.Int("mentors_matched", summary.MentorsMatched),
			zap.Int("emails_sent", summary.EmailsSent),
			zap.Int("email_failures", summary.EmailFailures),
		)
	}()

	log.Info("Cron job started")
	summary, err = job(ctx)
	switch {
	case err != nil:
		outcome = "error"
		log.Error("Cron job failed", zap.Error(err))
		errortracking.CaptureException(err, map[string]interface{}{"job": name})
	case summary.Skipped:
		outcome = "skipped"
		log.Info("Cron job skipped: non-production without DEV_EMAIL_OVERRIDE")
	}
	return summary, err
}

// Start launches the scheduler in its own goroutine.
func (c *Cron) Start() {
	c.cron.Start()
	logger.Info("Cron scheduler started")
}

// Stop stops scheduling new runs and returns a context that is done once
// all in-flight jobs have completed.
func (c *Cron) Stop() context.Context {
	return c.cron.Stop()
}

// Entries exposes the scheduled entries (used by tests).
func (c *Cron) Entries() []cron.Entry {
	return c.cron.Entries()
}
