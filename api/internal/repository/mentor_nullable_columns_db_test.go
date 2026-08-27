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
	"github.com/openmentor-io/openmentor/api/pkg/price"
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

// futureExpiry keeps the seeded login token consumable. The generic timestamptz
// filler writes a 2020 date, and ConsumeLoginToken checks the expiry in SQL, so
// without this override every case would look like an expired link.
const futureExpiry = "2999-01-01T00:00:00Z"

// stateMarkerColumns are nullable mentors columns whose non-NULL value does not
// DESCRIBE the mentor — it changes what the row IS. Filling them does not make a
// richer fixture, it makes a different kind of row, so seedFullMentor leaves
// them NULL.
//
// deleted_at is the only member: a non-NULL value means the profile is deleted
// (D70), and a deleted profile is deliberately invisible to GetByEmail,
// ConsumeLoginToken and the catalog. Filling it turned every subtest's fixture
// into a deleted mentor, so all three of those reads correctly refused it and
// the test blamed whichever profile column that subtest happened to be blanking.
//
// The columns are still ENUMERATED — the deleted_at subtest sets it back to NULL
// and proves a live profile reads fine, which is the property this file is for.
// If a future migration adds another marker of this kind, it will land here as
// the same wall of unrelated failures; add it to this set rather than weakening
// the query that is doing its job.
var stateMarkerColumns = map[string]bool{"deleted_at": true}

// seedFullMentor inserts an active mentor with every nullable column populated
// except the state markers above. Active because FetchAllMentorsFromDB — the
// catalog, the widest blast radius of the four — only selects active rows.
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

	fillable := make([]dbtest.Column, 0, len(columns))
	for _, c := range columns {
		if !stateMarkerColumns[c.Name] {
			fillable = append(fillable, c)
		}
	}

	written := dbtest.FillNullable(t, pool, "mentors", fillable, id, suffix, map[string]string{
		"login_token_expires_at": futureExpiry,
		// mentors_price_chk (000014) accepts only the canonical grammar, which
		// the generic per-column text filler cannot produce — without this the
		// seed INSERT fails and every subtest blames its own column.
		"price": price.Value{Kind: price.Fixed, Amount: 50}.String(),
	})

	// login_token has to hold a hash rather than the filler text, or
	// ConsumeLoginToken could never match the row.
	_, err = pool.Exec(ctx, `UPDATE mentors SET login_token = $1 WHERE id = $2`,
		HashOneTimeToken(loginTokenPlaintext), id)
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

			// Magic-link login. The atomic consumption reads only NOT NULL
			// columns, so the two lookup keys are the only NULLs that may stop it:
			// no token to match, or no expiry to compare against.
			consumed, err := repo.ConsumeLoginToken(ctx, loginTokenPlaintext)
			switch {
			case errors.Is(err, ErrTokenNotConsumable):
				if column.Name != "login_token" && column.Name != "login_token_expires_at" {
					t.Errorf("ConsumeLoginToken found no row with %s NULL", column.Name)
				}
			case err != nil:
				t.Errorf("ConsumeLoginToken with %s NULL: %v", column.Name, err)
			default:
				require.Equal(t, id, consumed.MentorID)
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
