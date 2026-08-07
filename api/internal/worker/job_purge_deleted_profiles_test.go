package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openmentor-io/openmentor/api/config"
	"github.com/openmentor-io/openmentor/api/pkg/metrics"
)

func purgeEnv(retentionDays int) *jobsTestEnv {
	return newJobsTestEnvWithConfig(func(cfg *config.Config) {
		cfg.Worker.ProfilePurgeRetentionDays = retentionDays
		cfg.Worker.ProfilePurgeCron = "0 15 3 * * *"
	})
}

func TestPurgeDeletedProfilesErasesEachProfileAndItsData(t *testing.T) {
	env := purgeEnv(30)
	deletedAt := time.Now().Add(-40 * 24 * time.Hour)
	env.repo.purgeableProfiles = []PurgeableProfile{
		{MentorID: "m1", DeletedAt: deletedAt},
		{MentorID: "m2", DeletedAt: deletedAt},
	}
	env.repo.purgeCounts["m1"] = PurgeCounts{Requests: 3, Reviews: 2, Invitations: 1}
	env.repo.purgeCounts["m2"] = PurgeCounts{Requests: 1}

	summary, err := env.handlers.PurgeDeletedProfiles(context.Background())
	if err != nil {
		t.Fatalf("PurgeDeletedProfiles returned error: %v", err)
	}

	if summary.MentorsMatched != 2 || summary.ProfilesPurged != 2 {
		t.Errorf("summary = %+v, want 2 matched / 2 purged", summary)
	}
	// The associated data the feature promises to remove is counted, not assumed.
	if summary.RequestsPurged != 4 || summary.ReviewsPurged != 2 || summary.InvitationsPurged != 1 {
		t.Errorf("summary counts = %d requests / %d reviews / %d invitations, want 4/2/1",
			summary.RequestsPurged, summary.ReviewsPurged, summary.InvitationsPurged)
	}

	// The configured retention reaches BOTH the listing and every erasure: the
	// second check is what stops a profile restored mid-run from being erased.
	for _, days := range env.repo.purgeRetentionDays {
		if days != 30 {
			t.Errorf("repository called with retention %d, want 30", days)
		}
	}

	// One event per erased profile, and only for the ones that committed.
	if got := env.tracker.count(); got != 2 {
		t.Errorf("tracked %d analytics events, want 2 (one per purged profile)", got)
	}
}

func TestPurgeDeletedProfilesSkipsProfilesRestoredMidRun(t *testing.T) {
	env := purgeEnv(30)
	env.repo.purgeableProfiles = []PurgeableProfile{
		{MentorID: "restored", DeletedAt: time.Now().Add(-40 * 24 * time.Hour)},
		{MentorID: "still-deleted", DeletedAt: time.Now().Add(-40 * 24 * time.Hour)},
	}
	// An admin restored this one between the listing and the write, so the
	// repository's retention re-check refuses it.
	env.repo.purgeErrs["restored"] = ErrProfileNotPurgeable

	summary, err := env.handlers.PurgeDeletedProfiles(context.Background())
	if err != nil {
		t.Fatalf("PurgeDeletedProfiles returned error: %v", err)
	}

	if summary.ProfilesSkipped != 1 || summary.ProfilesPurged != 1 {
		t.Errorf("summary = %+v, want 1 skipped / 1 purged", summary)
	}
	// A skip is not a failure: the guard did its job.
	if summary.PurgeFailures != 0 {
		t.Errorf("PurgeFailures = %d, want 0 — a restored profile is an expected outcome", summary.PurgeFailures)
	}
	if len(env.repo.purged) != 1 || env.repo.purged[0] != "still-deleted" {
		t.Errorf("purged = %v, want only [still-deleted]", env.repo.purged)
	}
	// No event for the profile that was not erased.
	if got := env.tracker.count(); got != 1 {
		t.Errorf("tracked %d events, want 1 — a skipped profile must not be reported as purged", got)
	}
}

func TestPurgeDeletedProfilesContinuesAfterOneFailure(t *testing.T) {
	env := purgeEnv(30)
	env.repo.purgeableProfiles = []PurgeableProfile{
		{MentorID: "boom", DeletedAt: time.Now().Add(-40 * 24 * time.Hour)},
		{MentorID: "fine-1", DeletedAt: time.Now().Add(-40 * 24 * time.Hour)},
		{MentorID: "fine-2", DeletedAt: time.Now().Add(-40 * 24 * time.Hour)},
	}
	env.repo.purgeErrs["boom"] = errors.New("lock timeout")

	summary, err := env.handlers.PurgeDeletedProfiles(context.Background())
	// One profile failing must not abort the backlog behind it, and must not
	// fail the run — the successes are committed and the failure retries
	// tomorrow.
	if err != nil {
		t.Fatalf("PurgeDeletedProfiles returned error: %v", err)
	}
	if summary.PurgeFailures != 1 || summary.ProfilesPurged != 2 {
		t.Errorf("summary = %+v, want 1 failure / 2 purged", summary)
	}
	if len(env.repo.purgeAttempts) != 3 {
		t.Errorf("attempted %d profiles, want all 3 attempted despite the failure", len(env.repo.purgeAttempts))
	}
}

func TestPurgeDeletedProfilesNoOpWhenNothingIsDue(t *testing.T) {
	env := purgeEnv(30)

	summary, err := env.handlers.PurgeDeletedProfiles(context.Background())
	if err != nil {
		t.Fatalf("PurgeDeletedProfiles returned error: %v", err)
	}
	if summary.MentorsMatched != 0 || summary.ProfilesPurged != 0 {
		t.Errorf("summary = %+v, want an empty run", summary)
	}
	if len(env.repo.purgeAttempts) != 0 {
		t.Errorf("attempted %d erasures on an empty list, want 0", len(env.repo.purgeAttempts))
	}
}

// The purge deliberately has NO non-production gate: it emails nobody, and
// gating it would leave the destructive path unexercised outside production.
func TestPurgeDeletedProfilesRunsOutsideProduction(t *testing.T) {
	env := newJobsTestEnvWithConfig(func(cfg *config.Config) {
		cfg.Server.AppEnv = "staging"
		cfg.Email.DevEmailOverride = ""
		cfg.Worker.ProfilePurgeRetentionDays = 30
		cfg.Worker.ProfilePurgeCron = "0 15 3 * * *"
	})
	env.repo.purgeableProfiles = []PurgeableProfile{
		{MentorID: "m1", DeletedAt: time.Now().Add(-40 * 24 * time.Hour)},
	}

	summary, err := env.handlers.PurgeDeletedProfiles(context.Background())
	if err != nil {
		t.Fatalf("PurgeDeletedProfiles returned error: %v", err)
	}
	if summary.Skipped {
		t.Error("purge must not use the non-production email gate")
	}
	if summary.ProfilesPurged != 1 {
		t.Errorf("ProfilesPurged = %d, want 1 outside production", summary.ProfilesPurged)
	}
}

// A retention that is unset or unparseable must never be read as "0 days past
// deletion", which would be every deleted profile.
func TestPurgeRetentionFallsBackInsteadOfErasingEverything(t *testing.T) {
	for _, configured := range []int{0, -1} {
		env := purgeEnv(configured)
		env.repo.purgeableProfiles = []PurgeableProfile{
			{MentorID: "m1", DeletedAt: time.Now().Add(-40 * 24 * time.Hour)},
		}

		if _, err := env.handlers.PurgeDeletedProfiles(context.Background()); err != nil {
			t.Fatalf("PurgeDeletedProfiles(%d) returned error: %v", configured, err)
		}
		for _, days := range env.repo.purgeRetentionDays {
			if days != defaultProfilePurgeRetentionDays {
				t.Errorf("retention %d configured -> repository got %d, want the %d-day default",
					configured, days, defaultProfilePurgeRetentionDays)
			}
		}
	}
}

// An empty schedule must not fail NewCron — that would stop the worker booting
// and take every transactional email with it.
func TestEmptyPurgeCronFallsBackToTheDefaultSchedule(t *testing.T) {
	env := newJobsTestEnvWithConfig(func(cfg *config.Config) {
		cfg.Worker.ProfilePurgeCron = ""
	})

	if _, err := NewCron(env.handlers); err != nil {
		t.Fatalf("NewCron failed with an empty purge schedule: %v", err)
	}

	var found bool
	for _, job := range env.handlers.CronJobs() {
		if job.Name == "purge-deleted-profiles" {
			found = true
			if job.Schedule != defaultProfilePurgeCron {
				t.Errorf("schedule = %q, want the default %q", job.Schedule, defaultProfilePurgeCron)
			}
			if !job.SkipIfRunning {
				t.Error("purge must set SkipIfRunning: a backlog can outlast the nightly interval")
			}
		}
	}
	if !found {
		t.Fatal("purge-deleted-profiles is not registered as a cron job")
	}
}

// ---------------------------------------------------------------------------
// Observability (D70)
// ---------------------------------------------------------------------------

// The gap this closes: PurgeDeletedProfiles returns nil even when individual
// profiles fail, so openmentor_worker_cron_runs_total records "success" for a
// sweep that erased nothing. The freshness gauge is what tells those apart, so
// a failed sweep must NOT advance it.
func TestPurgeFreshnessGaugeOnlyAdvancesOnACleanSweep(t *testing.T) {
	metrics.Init("test")
	due := PurgeableProfile{MentorID: "m1", DeletedAt: time.Now().Add(-40 * 24 * time.Hour)}

	t.Run("a sweep with failures leaves the gauge behind", func(t *testing.T) {
		metrics.ProfilePurgeLastSuccessTimestamp.Set(0)
		env := purgeEnv(30)
		env.repo.purgeableProfiles = []PurgeableProfile{due}
		env.repo.purgeErrs["m1"] = errors.New("lock timeout")

		summary, err := env.handlers.PurgeDeletedProfiles(context.Background())
		require.NoError(t, err, "the run still succeeds, which is exactly why the gauge matters")
		require.Equal(t, 1, summary.PurgeFailures)

		assert.Zero(t, gaugeValue(t, metrics.ProfilePurgeLastSuccessTimestamp),
			"a sweep that failed every profile must not count as a successful sweep")
	})

	t.Run("a clean sweep advances it", func(t *testing.T) {
		metrics.ProfilePurgeLastSuccessTimestamp.Set(0)
		env := purgeEnv(30)
		env.repo.purgeableProfiles = []PurgeableProfile{due}

		_, err := env.handlers.PurgeDeletedProfiles(context.Background())
		require.NoError(t, err)

		assert.InDelta(t, float64(time.Now().Unix()), gaugeValue(t, metrics.ProfilePurgeLastSuccessTimestamp), 60)
	})

	// The normal night. Treating "nothing due" as staleness would page every
	// deployment that simply has no deleted profiles.
	t.Run("an empty sweep advances it too", func(t *testing.T) {
		metrics.ProfilePurgeLastSuccessTimestamp.Set(0)
		env := purgeEnv(30)

		_, err := env.handlers.PurgeDeletedProfiles(context.Background())
		require.NoError(t, err)

		assert.InDelta(t, float64(time.Now().Unix()), gaugeValue(t, metrics.ProfilePurgeLastSuccessTimestamp), 60)
	})
}

func TestPurgeCountsEveryProfileOutcome(t *testing.T) {
	metrics.Init("test")
	env := purgeEnv(30)
	old := time.Now().Add(-40 * 24 * time.Hour)
	env.repo.purgeableProfiles = []PurgeableProfile{
		{MentorID: "ok", DeletedAt: old},
		{MentorID: "restored", DeletedAt: old},
		{MentorID: "boom", DeletedAt: old},
	}
	env.repo.purgeErrs["restored"] = ErrProfileNotPurgeable
	env.repo.purgeErrs["boom"] = errors.New("lock timeout")

	_, err := env.handlers.PurgeDeletedProfiles(context.Background())
	require.NoError(t, err)

	assert.Equal(t, 1.0, counterValue(t, purgeOutcomePurged))
	assert.Equal(t, 1.0, counterValue(t, purgeOutcomeSkipped))
	assert.Equal(t, 1.0, counterValue(t, purgeOutcomeFailed))
}

// The alert subtracts this gauge instead of comparing against a literal, so it
// has to track the configured schedule rather than a hardcoded nightly guess.
func TestPurgeMaxAgeGaugeFollowsTheConfiguredSchedule(t *testing.T) {
	metrics.Init("test")

	for _, tt := range []struct {
		name     string
		schedule string
		wantSecs float64
	}{
		{"nightly", "0 15 3 * * *", 2 * 24 * 60 * 60},
		{"twice daily", "0 0 3,15 * * *", 2 * 12 * 60 * 60},
		{"hourly", "0 0 * * * *", 2 * 60 * 60},
	} {
		t.Run(tt.name, func(t *testing.T) {
			env := newJobsTestEnvWithConfig(func(cfg *config.Config) {
				cfg.Worker.ProfilePurgeCron = tt.schedule
			})
			_, err := NewCron(env.handlers)
			require.NoError(t, err)

			assert.Equal(t, tt.wantSecs, gaugeValue(t, metrics.ProfilePurgeMaxAge),
				"max age must be two firing intervals of the configured schedule")
			assert.InDelta(t, float64(time.Now().Unix()),
				gaugeValue(t, metrics.ProfilePurgeFirstStartTimestamp), 60,
				"the grace window anchors at boot")
		})
	}
}

func gaugeValue(t *testing.T, g prometheus.Gauge) float64 {
	t.Helper()
	var m dto.Metric
	require.NoError(t, g.Write(&m))
	return m.GetGauge().GetValue()
}

func counterValue(t *testing.T, outcome string) float64 {
	t.Helper()
	var m dto.Metric
	c, err := metrics.ProfilePurgeProfilesTotal.GetMetricWithLabelValues(outcome)
	require.NoError(t, err)
	require.NoError(t, c.(prometheus.Metric).Write(&m))
	return m.GetCounter().GetValue()
}
