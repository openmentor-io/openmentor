package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openmentor-io/openmentor/api/internal/cache"
	"github.com/openmentor-io/openmentor/api/test/dbtest"
	"github.com/stretchr/testify/require"
)

// The mentors table is nullable almost everywhere with no defaults, and pgx v5
// fails the WHOLE row scan when a NULL lands in a non-pointer destination — so a
// single NULL column can make a mentor unreadable: no login, no public profile,
// and (while they are active) a broken catalog for everyone, because ScanMentors
// aborts on the first bad row.
//
// That is a property of the column list, not of any one column. The original
// report was "a NULL sort_order locks mentors out"; experience and price were
// exactly the same bug on two more columns, and the test that existed passed 0
// for sort_order and a NULL for nothing, so it could not have caught either.
// This test therefore enumerates the nullable columns out of the live schema
// instead of naming them: a nullable column added by a future migration and
// pulled into these SELECTs without a COALESCE fails here without anyone having
// to remember this file exists.
//
// Requires a database (see the dbtest package docs); skips without one.

// loginTokenPlaintext is the magic-link token the seeded row carries. Stored
// hashed, exactly as production does — the plaintext never reaches the database.
const loginTokenPlaintext = "nullable-column-test-token"

// seedFullMentor inserts an active mentor with EVERY nullable column populated.
// Active because FetchAllMentorsFromDB — the catalog, the widest blast radius of
// the four — only selects active rows.
func seedFullMentor(t *testing.T, pool *pgxpool.Pool, columns []dbtest.Column, suffix string) (id, mentorSlug, email string) {
	t.Helper()

	ctx := context.Background()
	mentorSlug = "nullscan-" + suffix
	err := pool.QueryRow(ctx, `
		INSERT INTO mentors (slug, name, status) VALUES ($1, 'Nullable Column Test', 'active')
		RETURNING id
	`, mentorSlug).Scan(&id)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM mentors WHERE id = $1`, id)
	})

	written := dbtest.FillNullable(t, pool, "mentors", columns, id, suffix, nil)

	// login_token has to hold a hash rather than the filler text, or
	// GetByLoginToken could never match the row.
	_, err = pool.Exec(ctx, `UPDATE mentors SET login_token = $1 WHERE id = $2`,
		HashLoginToken(loginTokenPlaintext), id)
	require.NoError(t, err)

	return id, mentorSlug, written["email"]
}

// TestEveryNullableMentorColumnStaysReadable blanks each nullable mentors column
// in turn and drives every read path that scans a mentor row. Fixing one column
// cannot satisfy it.
func TestEveryNullableMentorColumnStaysReadable(t *testing.T) {
	pool := dbtest.Pool(t)
	repo := NewMentorRepository(pool, cache.NewTagsCache(
		func(context.Context) (map[string]string, error) { return map[string]string{}, nil },
	))
	columns := dbtest.NullableColumns(t, pool, "mentors")
	ctx := context.Background()

	for i, column := range columns {
		t.Run(column.Name, func(t *testing.T) {
			suffix := fmt.Sprintf("%s-%d", strings.ReplaceAll(column.Name, "_", ""), i)
			id, mentorSlug, email := seedFullMentor(t, pool, columns, suffix)
			dbtest.SetNull(t, pool, "mentors", column.Name, id)

			// Login by email. A NULL email genuinely makes the row
			// unaddressable by email; that is the only acceptable "not found"
			// here, because every other column is a profile field that must not
			// cost the mentor their account.
			mentor, err := repo.GetByEmail(ctx, email)
			switch {
			case errors.Is(err, pgx.ErrNoRows):
				if column.Name != "email" {
					t.Errorf("GetByEmail found no row with %s NULL", column.Name)
				}
			case err != nil:
				t.Errorf("GetByEmail with %s NULL: %v", column.Name, err)
			default:
				require.Equal(t, id, mentor.MentorID)
			}

			// Magic-link login. Same shape: only the lookup key itself, and the
			// expiry the method deliberately rejects as missing, may fail.
			mentor, _, err = repo.GetByLoginToken(ctx, loginTokenPlaintext)
			switch {
			case errors.Is(err, pgx.ErrNoRows):
				if column.Name != "login_token" {
					t.Errorf("GetByLoginToken found no row with %s NULL", column.Name)
				}
			case err != nil:
				if column.Name != "login_token_expires_at" {
					t.Errorf("GetByLoginToken with %s NULL: %v", column.Name, err)
				}
			default:
				require.Equal(t, id, mentor.MentorID)
			}

			// Own-profile reads and the slug-history redirect target.
			mentor, err = repo.fetchMentorByUUIDFromDB(ctx, id)
			if err != nil {
				t.Errorf("fetchMentorByUUIDFromDB with %s NULL: %v", column.Name, err)
			} else {
				require.Equal(t, id, mentor.MentorID)
			}

			// Public profile page.
			mentor, err = repo.FetchSingleMentorFromDB(ctx, mentorSlug)
			if err != nil {
				t.Errorf("FetchSingleMentorFromDB with %s NULL: %v", column.Name, err)
			} else {
				require.Equal(t, id, mentor.MentorID)
			}

			// Public catalog — one bad active row breaks it for everybody.
			all, err := repo.FetchAllMentorsFromDB(ctx)
			if err != nil {
				t.Errorf("FetchAllMentorsFromDB with %s NULL: %v", column.Name, err)
				return
			}
			found := false
			for _, m := range all {
				if m.MentorID == id {
					found = true
					break
				}
			}
			require.True(t, found, "the catalog dropped the mentor with %s NULL", column.Name)
		})
	}
}
