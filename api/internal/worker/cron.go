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

	// SkipIfRunning drops a scheduled tick while the previous run of the SAME
	// job is still in flight, instead of starting a second concurrent pass.
	// Only worth setting for jobs whose runtime can approach their interval.
	SkipIfRunning bool
}

// Cron outcome labels for openmentor_worker_cron_runs_total. success / error /
// panic / skipped are emitted by runCronJob; skipped_overlap by the in-process
// chain (cronSkipLogger); these two by the lease.
const (
	cronOutcomeSkippedLease = "skipped_lease"
	cronOutcomeLeaseError   = "lease_error"
)

// CronJobs returns the scheduled jobs. The first four are ported from the func
// app's timer-triggered functions and keep its NCRONTAB expressions verbatim
// (6 fields, seconds first), taken from each function.json timer definition:
//
//	sessions-watcher            "0 30 8 * * *"    daily 08:30
//	update-status-reminder      "0 0 10 * * Wed"  Wednesdays 10:00
//	deactivate-pending-mentors  "0 0 10 * * Wed"  Wednesdays 10:00
//	randomize-sort-order        "0 0 1 * * *"     daily 01:00
//
// finalize-stuck-registrations has no func-app counterpart: it runs every 10
// minutes because it is the retry for a lost registration trigger, and a
// locked-out new mentor must not wait for a daily pass. Its interval is short
// enough that a pass emailing a long backlog can still be running when the next
// tick fires, so it is the one job registered with SkipIfRunning: two passes over
// the same replay set only race each other for the same rows. The daily/weekly
// jobs have no such margin.
//
// It stays the only one, deliberately: the wrapper is an in-process fast path for
// jobs whose runtime can approach their interval, and adding it to a weekly job
// whose scheduled runs cannot overlap would only add a way for a tick to be
// dropped.
//
// The wrapper is NOT what makes a job safe to run twice — it covers scheduled
// ticks only, and POST /jobs/cron/<name> bypasses it. Every job below is instead
// registered with a Postgres advisory LEASE (D82), taken in runGuardedCronJob at
// every entry point the scheduler and the manual trigger share, so "this named
// job runs at most once at a time" holds cluster-wide rather than per process.
// That is the invariant the row-level guards on the writes (FinalizeNewMentor,
// DeactivateMentor, SetRequestContactPending) cannot supply on their own:
// sessions-watcher and update-status-reminder WRITE NOTHING, so they have no row
// to claim, and two overlapping passes send every stale mentor two reminder
// emails. The lease is applied uniformly rather than per job so that a new job,
// or a new write inside an existing one, does not have to re-derive the argument.
//
// The lease is scoped to named scheduled jobs. The per-entity callbacks
// (/jobs/new-mentor-watcher and friends) stay unleased — they are keyed to one
// row, so the row-level claim is both necessary and sufficient there, and it is
// still what makes finalize-stuck-registrations safe against a concurrent
// new-mentor-watcher delivery.
func (h *Handlers) CronJobs() []CronJob {
	return []CronJob{
		{Name: "sessions-watcher", Schedule: "0 30 8 * * *", Run: h.SessionsWatcher},
		{Name: "update-status-reminder", Schedule: "0 0 10 * * Wed", Run: h.UpdateStatusReminder},
		{Name: "deactivate-pending-mentors", Schedule: "0 0 10 * * Wed", Run: h.DeactivatePendingMentors},
		{Name: "randomize-sort-order", Schedule: "0 0 1 * * *", Run: h.RandomizeSortOrder},
		{
			Name: "finalize-stuck-registrations", Schedule: "0 */10 * * * *",
			Run: h.FinalizeStuckRegistrations, SkipIfRunning: true,
		},
		// The only job whose schedule is configuration rather than a literal
		// (WORKER_PROFILE_PURGE_CRON): it erases data irreversibly, so an
		// operator must be able to move or effectively pause it without a
		// deploy. SkipIfRunning because a backlog — the first pass after this
		// ships, or after a retention change — can outlast a nightly interval,
		// and two passes over the same list would race each other for rows that
		// one of them has already erased.
		{
			Name: purgeDeletedProfilesJob, Schedule: h.profilePurgeCron,
			Run: h.PurgeDeletedProfiles, SkipIfRunning: true,
		},
	}
}

// NewCron builds the scheduler and registers every job in CronJobs.
func NewCron(h *Handlers) (*Cron, error) {
	// Azure NCRONTAB expressions include a seconds field, so use a
	// seconds-aware parser and keep the expressions verbatim.
	parser := cron.NewParser(
		cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow,
	)
	// UTC, not time.Local. A spec with no TZ= prefix parses to Location ==
	// time.Local, which SpecSchedule.Next treats as "the zone of the time I was
	// handed" — and the time it is handed is cron's own now(), i.e. time.Now() in
	// THIS location. So without the option every wall-clock schedule below ("daily
	// 08:30", "Wednesdays 10:00") fires at that time in the CONTAINER's local
	// zone, and a base-image change or a TZ env var silently moves every job away
	// from what the runbooks and the freshness alerts say it is.
	c := &Cron{cron: cron.New(cron.WithParser(parser), cron.WithLocation(time.UTC))}

	for _, job := range h.CronJobs() {
		schedule, err := parser.Parse(job.Schedule)
		if err != nil {
			return nil, err
		}
		if _, err := c.cron.AddJob(job.Schedule, scheduledJob(job, h.repo)); err != nil {
			return nil, err
		}
		if job.Name == purgeDeletedProfilesJob {
			// UTC for the same reason the scheduler runs in UTC: the interval is
			// measured off two Next() calls, and Next interprets a TZ-less spec in
			// the zone of the time it is given.
			publishPurgeFreshnessGauges(schedule, time.Now().UTC())
		}
		logger.Info("Registered cron job",
			zap.String("job", job.Name),
			zap.String("schedule", job.Schedule),
		)
	}

	return c, nil
}

// publishPurgeFreshnessGauges arms the ProfilePurgeStale alert at boot (D70).
//
// The staleness threshold is DERIVED from the schedule rather than configured
// separately: two firing intervals, measured off the parsed schedule itself. So
// retuning WORKER_PROFILE_PURGE_CRON moves the alert with it and the two can
// never disagree — the property DatabaseBackupStale gets by publishing
// openmentor_db_backup_max_age_seconds instead of hardcoding a duration.
//
// Two intervals, not one: a single missed sweep means data outlives its window
// by one night, which is not worth waking anybody for. Two consecutive misses
// means the job is not running, which is.
//
// Both gauges are published only when the scheduler actually registers the job.
// With WORKER_CRON_ENABLED=false nothing is published at all, and the companion
// ProfilePurgePipelineAbsent rule fires — which is the correct answer, because a
// worker with cron off is not enforcing retention.
func publishPurgeFreshnessGauges(schedule cron.Schedule, now time.Time) {
	first := schedule.Next(now)
	interval := schedule.Next(first).Sub(first)

	metrics.ProfilePurgeMaxAge.Set((2 * interval).Seconds())
	// Not a claim that a sweep happened — it is the "we only just started"
	// grace window, and the alert prefers last_success whenever that is set.
	metrics.ProfilePurgeFirstStartTimestamp.Set(float64(now.Unix()))

	logger.Info("Profile purge freshness gauges published",
		zap.Duration("schedule_interval", interval),
		zap.Duration("max_age", 2*interval),
	)
}

// scheduledJob turns one CronJob into the cron.Job the scheduler runs, applying
// the overlap guard when the job asked for it.
//
// The guard is per job (each wrapped job gets its own single-slot channel), so a
// long finalize pass never blocks an unrelated schedule. It only covers SCHEDULED
// ticks: the manual POST /jobs/cron/<name> trigger and the per-registration
// /jobs/new-mentor-watcher endpoint bypass it entirely. The lease inside
// runGuardedCronJob is what covers the first of those; the row-level claim in
// FinalizeNewMentor is what covers the second.
func scheduledJob(job CronJob, leaser jobLeaser) cron.Job {
	wrapped := cron.FuncJob(func() {
		runGuardedCronJob(context.Background(), job, leaser) //nolint:errcheck // logged + counted inside
	})
	if !job.SkipIfRunning {
		return wrapped
	}
	return cron.NewChain(cron.SkipIfStillRunning(cronSkipLogger{job: job.Name})).Then(wrapped)
}

// jobLeaser hands out the cross-process cron lease. Satisfied by JobsRepository,
// so the job handlers' existing fake repository can drive the skip path without a
// database.
type jobLeaser interface {
	AcquireJobLease(ctx context.Context, job string) (JobLease, error)
}

// runGuardedCronJob is the ONE entry point both the scheduler and
// POST /jobs/cron/<name> go through, so the lease it takes is what makes
// "this named job runs at most once at a time" true of the job rather than
// of the scheduler (D82).
//
// It fails CLOSED. A lease that cannot be acquired means the answer to "is a
// pass already running?" is unknown, and every one of these jobs either emails
// real people or erases data, so guessing "no" is the expensive guess. Every job
// is also DB-driven, so a database that cannot lend a connection is one the run
// would have failed against anyway — the lease just fails it earlier, with a
// distinguishable outcome label instead of a mid-pass error.
func runGuardedCronJob(ctx context.Context, job CronJob, leaser jobLeaser) (JobSummary, error) {
	lease, err := leaser.AcquireJobLease(ctx, job.Name)
	if err != nil {
		metrics.WorkerCronRunsTotal.WithLabelValues(job.Name, cronOutcomeLeaseError).Inc()
		logger.Error("Cron job lease could not be acquired",
			zap.String("job", job.Name),
			logger.RedactedError(err),
		)
		return JobSummary{Job: job.Name}, fmt.Errorf("acquire lease for cron job %s: %w", job.Name, err)
	}
	if !lease.Acquired {
		metrics.WorkerCronRunsTotal.WithLabelValues(job.Name, cronOutcomeSkippedLease).Inc()
		logger.Warn("Cron job skipped: another run holds the lease", zap.String("job", job.Name))
		return JobSummary{Job: job.Name, Skipped: true, SkipReason: skipReasonLeaseHeld}, nil
	}
	defer lease.Release()

	return runCronJob(ctx, job.Name, job.Run)
}

// cronSkipLogger surfaces the overlap guard's decision. cron only logs through it
// when a tick is dropped, so a dropped tick becomes a warning plus a run with the
// "skipped_overlap" outcome — otherwise the pass would silently vanish from
// openmentor_worker_cron_runs_total and look like a scheduler that stopped firing.
type cronSkipLogger struct{ job string }

func (l cronSkipLogger) Info(msg string, _ ...interface{}) {
	logger.Warn("Cron tick dropped: previous run still in flight",
		zap.String("job", l.job),
		zap.String("cron_message", msg),
	)
	metrics.WorkerCronRunsTotal.WithLabelValues(l.job, "skipped_overlap").Inc()
}

func (l cronSkipLogger) Error(err error, msg string, _ ...interface{}) {
	logger.Error("Cron scheduler error",
		zap.String("job", l.job),
		zap.String("cron_message", msg),
		logger.RedactedError(err),
	)
}

// RegisterCronRoutes exposes every cron job as a manually-triggerable
// endpoint - POST /jobs/cron/<name> - that runs the same function once and
// returns its JobSummary as JSON (500 with the partial summary on error).
// Invaluable for staging smoke tests. The routes sit in the /jobs group,
// so the same X-Worker-Token middleware guards them.
func (s *Server) RegisterCronRoutes(h *Handlers) {
	for _, job := range h.CronJobs() {
		s.jobs.POST("/cron/"+job.Name, func(c *gin.Context) {
			summary, err := runGuardedCronJob(c.Request.Context(), job, h.repo)
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
		log.Error("Cron job failed", logger.RedactedError(err))
		errortracking.CaptureException(err, map[string]interface{}{"job": name})
	case summary.Skipped:
		outcome = "skipped"
		if summary.SkipReason == "" {
			summary.SkipReason = skipReasonNonProduction
		}
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

// Location is the zone the scheduler evaluates every spec in, and the zone a
// test has to hand SpecSchedule.Next to reproduce what the run loop does.
func (c *Cron) Location() *time.Location {
	return c.cron.Location()
}
