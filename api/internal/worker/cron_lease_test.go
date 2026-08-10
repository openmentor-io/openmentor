package worker

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/robfig/cron/v3"
)

// grantingLeaser always hands out the lease. It stands in for a healthy database
// in the tests that are about a layer OTHER than the lease.
type grantingLeaser struct {
	acquired atomic.Int32
	released atomic.Int32
}

func (l *grantingLeaser) AcquireJobLease(context.Context, string) (JobLease, error) {
	l.acquired.Add(1)
	return JobLease{Acquired: true, release: func() { l.released.Add(1) }}, nil
}

// heldLeaser is a database where another run of this job already holds the lease.
type heldLeaser struct{ jobs []string }

func (l *heldLeaser) AcquireJobLease(_ context.Context, job string) (JobLease, error) {
	l.jobs = append(l.jobs, job)
	return JobLease{}, nil
}

// erroringLeaser is a database that cannot answer "is a pass already running?".
type erroringLeaser struct{ err error }

func (l *erroringLeaser) AcquireJobLease(context.Context, string) (JobLease, error) {
	return JobLease{}, l.err
}

// TestRunGuardedCronJobSkipsWhenLeaseHeld is the case the in-process
// SkipIfStillRunning chain cannot cover: a manual POST /jobs/cron/<name> arriving
// while a scheduled tick of the same job is still working. Without the lease the
// handler simply runs a second time, and for sessions-watcher /
// update-status-reminder — which write nothing and so have no row to claim — that
// is a second reminder email to every stale mentor.
func TestRunGuardedCronJobSkipsWhenLeaseHeld(t *testing.T) {
	var runs atomic.Int32
	leaser := &heldLeaser{}

	summary, err := runGuardedCronJob(context.Background(), CronJob{
		Name: "sessions-watcher",
		Run: func(context.Context) (JobSummary, error) {
			runs.Add(1)
			return JobSummary{Job: "sessions-watcher", EmailsSent: 4}, nil
		},
	}, leaser)

	if err != nil {
		t.Fatalf("a lease held by another run is not an error: %v", err)
	}
	if got := runs.Load(); got != 0 {
		t.Errorf("handler ran %d times while the lease was held, want 0", got)
	}
	if !summary.Skipped || summary.SkipReason != skipReasonLeaseHeld {
		t.Errorf("summary = %+v, want Skipped with SkipReason %q", summary, skipReasonLeaseHeld)
	}
	if summary.EmailsSent != 0 {
		t.Errorf("summary reported %d emails for a run that never happened", summary.EmailsSent)
	}
	if len(leaser.jobs) != 1 || leaser.jobs[0] != "sessions-watcher" {
		t.Errorf("lease asked for %v, want [sessions-watcher]", leaser.jobs)
	}
}

// The lease fails CLOSED: an unknown answer to "is a pass already running?" must
// not become "no". A manual trigger sees the 500 rather than a silent no-op.
func TestRunGuardedCronJobFailsClosedOnLeaseError(t *testing.T) {
	var runs atomic.Int32
	leaseErr := errors.New("pool exhausted")

	_, err := runGuardedCronJob(context.Background(), CronJob{
		Name: "purge-deleted-profiles",
		Run: func(context.Context) (JobSummary, error) {
			runs.Add(1)
			return JobSummary{}, nil
		},
	}, &erroringLeaser{err: leaseErr})

	if !errors.Is(err, leaseErr) {
		t.Fatalf("err = %v, want it to wrap %v", err, leaseErr)
	}
	if got := runs.Load(); got != 0 {
		t.Errorf("handler ran %d times behind an unusable lease, want 0", got)
	}
}

// A lease that was acquired must be released even when the handler panics —
// otherwise one panicking pass leaves the job locked out until the process exits.
func TestRunGuardedCronJobReleasesLeaseOnPanic(t *testing.T) {
	leaser := &grantingLeaser{}

	_, err := runGuardedCronJob(context.Background(), CronJob{
		Name: "finalize-stuck-registrations",
		Run:  func(context.Context) (JobSummary, error) { panic("boom") },
	}, leaser)

	if err == nil {
		t.Fatal("a panicking job must return an error")
	}
	if got := leaser.released.Load(); got != 1 {
		t.Errorf("lease released %d times after a panic, want 1", got)
	}
}

// Every cron job goes through the lease. This is the assertion that stops a new
// job being added without one — the invariant is uniform on purpose, so nobody
// has to re-derive "are this job's writes individually replay-safe?" (D82).
func TestEveryCronJobTakesTheLease(t *testing.T) {
	env := newJobsTestEnv()

	for _, job := range env.handlers.CronJobs() {
		before := len(env.repo.leasesRequested)
		if _, err := runGuardedCronJob(context.Background(), job, env.repo); err != nil {
			t.Fatalf("%s: %v", job.Name, err)
		}
		if len(env.repo.leasesRequested) != before+1 {
			t.Errorf("%s ran without asking for a lease", job.Name)
		}
	}
	if env.repo.leasesReleased != len(env.handlers.CronJobs()) {
		t.Errorf("released %d leases for %d runs", env.repo.leasesReleased, len(env.handlers.CronJobs()))
	}
}

// TestCronSchedulesAreUTC: the schedules are wall-clock times ("daily 08:30",
// "Wednesdays 10:00") and a spec with no TZ= prefix is evaluated in the zone of
// the time the scheduler hands it — time.Now() in the Cron's own location. So
// without cron.WithLocation(time.UTC) the whole table silently follows the
// container's zone, and a base-image or TZ change moves every job.
//
// time.Local is repointed rather than TZ being set, because the runtime resolves
// TZ once at init: setting the env var inside a test changes nothing.
func TestCronSchedulesAreUTC(t *testing.T) {
	original := time.Local
	time.Local = time.FixedZone("TEST-07", -7*60*60)
	t.Cleanup(func() { time.Local = original })

	env := newJobsTestEnv()
	c, err := NewCron(env.handlers)
	if err != nil {
		t.Fatalf("NewCron: %v", err)
	}

	if c.Location() != time.UTC {
		t.Fatalf("scheduler location = %v, want UTC", c.Location())
	}

	jobs := env.handlers.CronJobs()
	entries := c.Entries()
	// The daily 08:30 job, resolved the way the run loop does it: Next(now in the
	// scheduler's location).
	var checked bool
	for i, job := range jobs {
		if job.Name != "sessions-watcher" {
			continue
		}
		next := entries[i].Schedule.Next(time.Now().In(c.Location())).UTC()
		if next.Hour() != 8 || next.Minute() != 30 {
			t.Errorf("sessions-watcher next run = %s, want 08:30 UTC", next.Format(time.RFC3339))
		}
		checked = true
	}
	if !checked {
		t.Fatal("sessions-watcher is no longer registered; this test needs updating")
	}
}

// A spec is only ever parsed by the seconds-aware parser, so a TZ-less spec must
// come out carrying time.Local — the premise the test above rests on. If a cron
// upgrade changes that default, this fails and says so directly.
func TestParsedSpecsInheritTheSchedulerLocation(t *testing.T) {
	parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	schedule, err := parser.Parse("0 30 8 * * *")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	spec, ok := schedule.(*cron.SpecSchedule)
	if !ok {
		t.Fatalf("parsed schedule is %T, want *cron.SpecSchedule", schedule)
	}
	if spec.Location != time.Local {
		t.Errorf("TZ-less spec parsed with Location %v, want time.Local "+
			"(cron changed its default; WithLocation may no longer be what pins the zone)", spec.Location)
	}
}
