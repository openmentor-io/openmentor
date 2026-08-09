package worker

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// The cron lease's exclusivity is a Postgres property — pg_try_advisory_lock
// deciding a winner between sessions that never see each other's state — so no
// fake can model it. These tests need a real database and skip without one; see
// the dbtest package for how to point them at one.

// TestJobLeaseAdmitsExactlyOneConcurrentCaller is the property the whole of D82
// rests on: N callers ask for the same job's lease at once, one gets it.
func TestJobLeaseAdmitsExactlyOneConcurrentCaller(t *testing.T) {
	repo := testRepository(t)
	const callers = 8
	const job = "lease-test-exclusive"

	var acquired atomic.Int32
	var releases []JobLease
	var mu sync.Mutex

	var start sync.WaitGroup
	start.Add(1)
	var done sync.WaitGroup
	done.Add(callers)

	for range callers {
		go func() {
			defer done.Done()
			start.Wait()
			lease, err := repo.AcquireJobLease(context.Background(), job)
			if err != nil {
				return
			}
			if lease.Acquired {
				acquired.Add(1)
				mu.Lock()
				releases = append(releases, lease)
				mu.Unlock()
			}
		}()
	}

	start.Done()
	done.Wait()

	require.Equal(t, int32(1), acquired.Load(),
		"exactly one of %d concurrent callers may hold the lease", callers)

	for _, lease := range releases {
		lease.Release()
	}
}

// The lease is a skip, not a permanent lock: once the holder releases it the next
// run gets it. A lease that never came back would silently stop the job forever.
func TestJobLeaseIsReacquirableAfterRelease(t *testing.T) {
	repo := testRepository(t)
	const job = "lease-test-reacquire"

	first, err := repo.AcquireJobLease(context.Background(), job)
	require.NoError(t, err)
	require.True(t, first.Acquired)

	blocked, err := repo.AcquireJobLease(context.Background(), job)
	require.NoError(t, err)
	require.False(t, blocked.Acquired, "the lease must be exclusive while it is held")

	first.Release()

	second, err := repo.AcquireJobLease(context.Background(), job)
	require.NoError(t, err)
	require.True(t, second.Acquired, "the lease must be available again after Release")
	second.Release()
}

// Per job, not global: a long finalize pass must not lock out the nightly purge.
// hashtext collisions would make this fail, which is the point of asserting it.
func TestJobLeasesAreIndependentPerJob(t *testing.T) {
	repo := testRepository(t)

	first, err := repo.AcquireJobLease(context.Background(), "lease-test-job-a")
	require.NoError(t, err)
	require.True(t, first.Acquired)
	defer first.Release()

	second, err := repo.AcquireJobLease(context.Background(), "lease-test-job-b")
	require.NoError(t, err)
	require.True(t, second.Acquired, "a different job's lease must be independent")
	second.Release()
}

// Every registered job name must map to a distinct lock key. hashtext can
// collide, and a collision would mean two unrelated jobs silently excluding each
// other — a job that stops running, with no error anywhere.
func TestRegisteredJobNamesDoNotCollide(t *testing.T) {
	repo := testRepository(t)
	env := newJobsTestEnv()

	seen := map[int32]string{}
	for _, job := range env.handlers.CronJobs() {
		var key int32
		require.NoError(t, repo.pool.QueryRow(context.Background(),
			`SELECT hashtext($1)`, job.Name).Scan(&key))
		if other, clash := seen[key]; clash {
			t.Fatalf("%q and %q hash to the same lease key %d", job.Name, other, key)
		}
		seen[key] = job.Name
	}
}
