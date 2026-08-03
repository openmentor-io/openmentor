package worker

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/openmentor-io/openmentor/api/test/dbtest"
	"github.com/stretchr/testify/require"
)

// Same defect class as the repository-side nullable-column tests: every JobRequest
// and JobReview field is a plain string, and pgx fails the WHOLE row scan on a
// NULL in a non-pointer destination — so an un-COALESCEd nullable column stops a
// job reading that request at all. For the two reminder crons the loss is not
// even scoped to the bad row: listJobReminderRequests aborts the whole list, so
// one request costs the mentor their entire reminder email.
//
// Requires a database (see the dbtest package docs); skips without one.

// seedFullJobRequest inserts a mentor, a request with every nullable column
// populated, and the review the process-review job reads. The request is aged
// past both reminder-cron thresholds so the list queries select it.
func seedFullJobRequest(t *testing.T, repo *Repository, columns []dbtest.Column, suffix, status string) (mentorID, requestID, reviewID string) {
	t.Helper()

	ctx := context.Background()
	pool := repo.pool
	err := pool.QueryRow(ctx, `
		INSERT INTO mentors (slug, name, status) VALUES ($1, 'Job Request Owner', 'active')
		RETURNING id
	`, "jobnullscan-"+suffix).Scan(&mentorID)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM mentors WHERE id = $1`, mentorID)
	})

	err = pool.QueryRow(ctx, `
		INSERT INTO client_requests (mentor_id, status, created_at)
		VALUES ($1, $2, NOW() - INTERVAL '30 days')
		RETURNING id
	`, mentorID, status).Scan(&requestID)
	require.NoError(t, err)
	// By id, not by mentor_id: the mentor_id case NULLs exactly that column, and
	// a cleanup keyed on it would leak the row and collide on the next run's
	// UNIQUE airtable_id. client_requests.mentor_id is ON DELETE SET NULL, so the
	// mentor cleanup above would not take the request with it either.
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM client_requests WHERE id = $1`, requestID)
	})

	// Two columns the generic per-type filler cannot satisfy: decline_reason is
	// CHECK-constrained to an enum, and mentor_id has to be a real mentor rather
	// than a placeholder UUID (FK, and the reminder crons select on it).
	// status_changed_at is aged so ListStaleInProgressRequests' 120-hour
	// predicate selects the row.
	dbtest.FillNullable(t, pool, "client_requests", columns, requestID, suffix, map[string]string{
		"decline_reason":    "other",
		"mentor_id":         mentorID,
		"status_changed_at": "2020-01-02T03:04:05Z",
	})

	err = pool.QueryRow(ctx,
		`INSERT INTO reviews (client_request_id, mentor_review) VALUES ($1, 'good session') RETURNING id`,
		requestID).Scan(&reviewID)
	require.NoError(t, err)

	return mentorID, requestID, reviewID
}

// TestEveryNullableClientRequestColumnStaysReadableByJobs blanks each nullable
// client_requests column in turn and drives every worker read over that table.
func TestEveryNullableClientRequestColumnStaysReadableByJobs(t *testing.T) {
	repo := testRepository(t)
	columns := dbtest.NullableColumns(t, repo.pool, "client_requests")
	ctx := context.Background()

	for i, column := range columns {
		t.Run(column.Name, func(t *testing.T) {
			suffix := fmt.Sprintf("%s-%d", strings.ReplaceAll(column.Name, "_", ""), i)
			// 'pending' feeds the sessions-watcher reminder, 'working' the
			// update-status reminder; one row cannot be in both status sets, so
			// each cron gets its own.
			pendingMentor, pendingRequest, reviewID := seedFullJobRequest(t, repo, columns, "pending-"+suffix, "pending")
			workingMentor, workingRequest, _ := seedFullJobRequest(t, repo, columns, "working-"+suffix, "working")
			dbtest.SetNull(t, repo.pool, "client_requests", column.Name, pendingRequest)
			dbtest.SetNull(t, repo.pool, "client_requests", column.Name, workingRequest)

			// new-request-watcher.
			request, err := repo.GetJobRequestByID(ctx, pendingRequest)
			if err != nil {
				t.Errorf("GetJobRequestByID with %s NULL: %v", column.Name, err)
			} else {
				require.NotNil(t, request, "GetJobRequestByID lost the row with %s NULL", column.Name)
			}

			// request-process-finished. The INNER JOIN on the mentor means a
			// detached request legitimately reads as "not found".
			request, err = repo.GetJobRequestWithMentorName(ctx, pendingRequest)
			switch {
			case err != nil:
				t.Errorf("GetJobRequestWithMentorName with %s NULL: %v", column.Name, err)
			case request == nil && column.Name != "mentor_id":
				t.Errorf("GetJobRequestWithMentorName lost the row with %s NULL", column.Name)
			}

			// process-mentee-review.
			review, err := repo.GetJobReviewByID(ctx, reviewID)
			if err != nil {
				t.Errorf("GetJobReviewByID with %s NULL: %v", column.Name, err)
			} else {
				require.NotNil(t, review, "GetJobReviewByID lost the row with %s NULL", column.Name)
			}

			// sessions-watcher reminder. mentor_id detaches the row from the
			// mentor and status_changed_at is what the 120-hour predicate of the
			// other cron filters on, so those two may legitimately empty a list.
			reminders, err := repo.ListStalePendingRequests(ctx, pendingMentor)
			switch {
			case err != nil:
				t.Errorf("ListStalePendingRequests with %s NULL: %v", column.Name, err)
			case len(reminders) == 0 && column.Name != "mentor_id":
				t.Errorf("ListStalePendingRequests dropped the request with %s NULL", column.Name)
			}

			// update-status-reminder.
			reminders, err = repo.ListStaleInProgressRequests(ctx, workingMentor)
			switch {
			case err != nil:
				t.Errorf("ListStaleInProgressRequests with %s NULL: %v", column.Name, err)
			case len(reminders) == 0 && column.Name != "mentor_id" && column.Name != "status_changed_at":
				t.Errorf("ListStaleInProgressRequests dropped the request with %s NULL", column.Name)
			}
		})
	}
}
