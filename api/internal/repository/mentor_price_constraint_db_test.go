package repository

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/openmentor-io/openmentor/api/pkg/price"
	"github.com/openmentor-io/openmentor/api/test/dbtest"
	"github.com/stretchr/testify/require"
)

// The price grammar exists in three places that cannot import each other: the
// mentors_price_chk regexp added by migration 000014, pkg/price.Parse behind the
// `price` binding tag, and web/src/lib/price.ts in the browser. Only the first
// is load-bearing — it is the one that cannot be bypassed — but a disagreement
// between them is not a rejected value, it is a save that gets past the form and
// the API and then fails in the repository as a 500 naming no field.
//
// So this does not assert a hand-written list of bad strings against SQL. It
// runs the SAME table through both the constraint and the parser and requires
// them to reach the same verdict, which is what makes it fail when someone
// edits one grammar and not the other.
//
// Requires a database (see the dbtest package docs); skips without one.

// priceGrammarCases mirrors parseCases in pkg/price/price_test.go and the table
// in web/src/lib/__tests__/price.test.ts. Add to all three together.
var priceGrammarCases = []string{
	// Accepted.
	"Free",
	"Negotiable",
	"$1",
	"$10",
	"$50",
	"$100",
	"$120",
	"$200",
	"$1000",

	// Rejected.
	"",
	"$",
	"$0",
	"$00",
	"$01",
	"$050",
	"$1001",
	"$99999",
	"$+50",
	"$-50",
	"50",
	"$50.00",
	"$1,000",
	"$50 / hour",
	"$100 / hour",
	"$ 50",
	" Free",
	"Free ",
	"Free\n",
	"free",
	"FREE",
	"negotiable",
	"Negotiable price",
	"$٥٠",
	"€50",
}

func TestPriceConstraintAgreesWithTheParser(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()

	for i, raw := range priceGrammarCases {
		t.Run(fmt.Sprintf("%02d/%s", i, raw), func(t *testing.T) {
			slug := fmt.Sprintf("price-chk-%02d", i)
			var id string
			err := pool.QueryRow(ctx, `
				INSERT INTO mentors (slug, name, status, price)
				VALUES ($1, 'Price Constraint Test', 'active', $2)
				RETURNING id
			`, slug, raw).Scan(&id)
			if err == nil {
				t.Cleanup(func() {
					_, _ = pool.Exec(context.Background(), `DELETE FROM mentors WHERE id = $1`, id)
				})
			}

			storedOK := err == nil
			parsedOK := price.IsCanonical(raw)

			require.Equal(t, parsedOK, storedOK,
				"mentors_price_chk and price.Parse disagree on %q: database accepted=%v, parser accepted=%v (err=%v)",
				raw, storedOK, parsedOK, err)

			if !storedOK {
				require.Contains(t, err.Error(), "mentors_price_chk",
					"%q was rejected by something other than the price constraint", raw)
			}
		})
	}
}

// The constraint is what makes the grammar unbypassable, so prove it is on the
// column rather than inferring it from the inserts above — a migration that
// silently failed to add it would otherwise show up as "every value accepted",
// which the table would catch only via its rejected half.
func TestPriceConstraintExists(t *testing.T) {
	pool := dbtest.Pool(t)

	var definition string
	err := pool.QueryRow(context.Background(), `
		SELECT pg_get_constraintdef(oid)
		FROM pg_constraint
		WHERE conrelid = 'mentors'::regclass AND conname = 'mentors_price_chk'
	`).Scan(&definition)
	require.NoError(t, err, "mentors_price_chk is missing; migration 000014 did not apply")

	// The bounds are the part most likely to be "tidied" without re-reading why
	// $0 is excluded: a free session is Free, which is a different statement.
	require.Contains(t, definition, "Free")
	require.Contains(t, definition, "Negotiable")
	require.Contains(t, definition, "1000")
}

// A NULL price is deliberately still allowed: migration 000014 adds a CHECK, not
// NOT NULL, so the column's nullability is exactly as D50 left it and the
// COALESCEs in mentorSelect stay load-bearing. Production held no NULL prices
// when the constraint shipped, and every write path binds a `required` field —
// but the constraint does not enforce that, and a test that assumed it did would
// be asserting something false.
func TestPriceConstraintStillPermitsNull(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()

	var id string
	err := pool.QueryRow(ctx, `
		INSERT INTO mentors (slug, name, status, price)
		VALUES ('price-chk-null', 'Price Constraint Test', 'active', NULL)
		RETURNING id
	`).Scan(&id)
	require.NoError(t, err, "a CHECK is satisfied by NULL; 000014 must not have added NOT NULL")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM mentors WHERE id = $1`, id)
	})
}

// Every price live in production when the grammar shipped (2026-08-13) still
// stores. The migration's normalisation was a no-op on this data, so if any of
// these stops being accepted the constraint has been narrowed past what real
// mentors already chose.
func TestProductionPricesStillStore(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()

	live := []string{
		"Negotiable", "$50", "Free", "$100", "$30", "$150", "$20",
		"$10", "$40", "$70", "$60", "$200", "$90", "$120", "$80",
	}

	for i, raw := range live {
		t.Run(strings.TrimPrefix(raw, "$"), func(t *testing.T) {
			var id string
			err := pool.QueryRow(ctx, `
				INSERT INTO mentors (slug, name, status, price)
				VALUES ($1, 'Live Price Test', 'active', $2)
				RETURNING id
			`, fmt.Sprintf("price-live-%02d", i), raw).Scan(&id)
			require.NoError(t, err, "a price that exists in production no longer stores: %q", raw)
			t.Cleanup(func() {
				_, _ = pool.Exec(context.Background(), `DELETE FROM mentors WHERE id = $1`, id)
			})
		})
	}
}
