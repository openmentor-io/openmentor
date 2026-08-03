package repository

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openmentor-io/openmentor/api/internal/models"
	"github.com/openmentor-io/openmentor/api/test/dbtest"
	"github.com/stretchr/testify/require"
)

// The mentors-table version of this test is
// mentor_nullable_columns_db_test.go; the same defect class applies to
// client_requests, which is nullable nearly everywhere too. A single NULL in a
// column that clientRequestSelect does not COALESCE fails the pgx scan and
// empties the mentor's whole request inbox, not just the one request.
//
// Requires a database (see the dbtest package docs); skips without one.

// seedFullClientRequest inserts a request with EVERY nullable column populated,
// plus the review the LEFT JOIN picks up.
func seedFullClientRequest(t *testing.T, pool *pgxpool.Pool, columns []dbtest.Column, suffix string) (mentorID, requestID string) {
	t.Helper()

	ctx := context.Background()
	err := pool.QueryRow(ctx, `
		INSERT INTO mentors (slug, name, status) VALUES ($1, 'Request Owner', 'active')
		RETURNING id
	`, "reqnullscan-"+suffix).Scan(&mentorID)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM mentors WHERE id = $1`, mentorID)
	})

	err = pool.QueryRow(ctx, `
		INSERT INTO client_requests (mentor_id, status) VALUES ($1, 'pending')
		RETURNING id
	`, mentorID).Scan(&requestID)
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
	// than a placeholder UUID (FK, and the by-mentor reads look the row up by it).
	dbtest.FillNullable(t, pool, "client_requests", columns, requestID, suffix,
		map[string]string{"decline_reason": "other", "mentor_id": mentorID})

	_, err = pool.Exec(ctx,
		`INSERT INTO reviews (client_request_id, mentor_review) VALUES ($1, 'good session')`, requestID)
	require.NoError(t, err)

	return mentorID, requestID
}

// TestEveryNullableClientRequestColumnStaysReadable blanks each nullable
// client_requests column in turn and drives every read over
// clientRequestSelect.
func TestEveryNullableClientRequestColumnStaysReadable(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := NewClientRequestRepository(pool)
	columns := dbtest.NullableColumns(t, pool, "client_requests")
	ctx := context.Background()

	for i, column := range columns {
		t.Run(column.Name, func(t *testing.T) {
			suffix := fmt.Sprintf("%s-%d", strings.ReplaceAll(column.Name, "_", ""), i)
			mentorID, requestID := seedFullClientRequest(t, pool, columns, suffix)
			dbtest.SetNull(t, pool, "client_requests", column.Name, requestID)

			// Mentor's own inbox, and the admin view of the same list.
			for name, list := range map[string]func() ([]*models.MentorClientRequest, error){
				"GetByMentor": func() ([]*models.MentorClientRequest, error) {
					return repo.GetByMentor(ctx, mentorID, models.ActiveStatuses)
				},
				"ListByMentorFiltered": func() ([]*models.MentorClientRequest, error) {
					return repo.ListByMentorFiltered(ctx, mentorID, nil)
				},
			} {
				requests, err := list()
				if err != nil {
					t.Errorf("%s with %s NULL: %v", name, column.Name, err)
					continue
				}
				// A NULL mentor_id genuinely detaches the request from the
				// mentor, so it is the only column allowed to drop out of a
				// by-mentor list.
				if len(requests) == 0 && column.Name != "mentor_id" {
					t.Errorf("%s dropped the request with %s NULL", name, column.Name)
				}
			}

			// Single-request read (status changes, decline flow, admin detail).
			request, err := repo.GetByID(ctx, requestID)
			if err != nil {
				t.Errorf("GetByID with %s NULL: %v", column.Name, err)
			} else {
				require.Equal(t, requestID, request.ID)
			}
		})
	}
}
