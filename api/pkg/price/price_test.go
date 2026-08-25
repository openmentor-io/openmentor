package price_test

import (
	"testing"

	"github.com/openmentor-io/openmentor/api/pkg/price"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// parseCases is the definition of the grammar, and three implementations have
// to agree with it: this package, the mentors_price_chk regexp in
// api/migrations/000014_price_grammar.up.sql (proved against a real database by
// api/internal/repository/mentor_price_constraint_db_test.go) and the browser
// copy in web/src/lib/price.ts (pinned by web/src/lib/__tests__/price.test.ts,
// which carries this same table). A value one accepts and another rejects is a
// save that 400s with no field attached, or a row the catalog cannot classify.
var parseCases = []struct {
	raw    string
	want   price.Value
	valid  bool
	reason string
}{
	{raw: "Free", want: price.Value{Kind: price.Free}, valid: true},
	{raw: "Negotiable", want: price.Value{Kind: price.Negotiable}, valid: true},

	{raw: "$1", want: price.Value{Kind: price.Fixed, Amount: 1}, valid: true, reason: "MinAmount"},
	{raw: "$1000", want: price.Value{Kind: price.Fixed, Amount: 1000}, valid: true, reason: "MaxAmount, inclusive"},
	// Every distinct amount live in production on 2026-08-13.
	{raw: "$10", want: price.Value{Kind: price.Fixed, Amount: 10}, valid: true},
	{raw: "$20", want: price.Value{Kind: price.Fixed, Amount: 20}, valid: true},
	{raw: "$30", want: price.Value{Kind: price.Fixed, Amount: 30}, valid: true},
	{raw: "$40", want: price.Value{Kind: price.Fixed, Amount: 40}, valid: true},
	{raw: "$50", want: price.Value{Kind: price.Fixed, Amount: 50}, valid: true},
	{raw: "$60", want: price.Value{Kind: price.Fixed, Amount: 60}, valid: true},
	{raw: "$70", want: price.Value{Kind: price.Fixed, Amount: 70}, valid: true},
	{raw: "$80", want: price.Value{Kind: price.Fixed, Amount: 80}, valid: true},
	{raw: "$90", want: price.Value{Kind: price.Fixed, Amount: 90}, valid: true},
	{raw: "$100", want: price.Value{Kind: price.Fixed, Amount: 100}, valid: true},
	{raw: "$120", want: price.Value{Kind: price.Fixed, Amount: 120}, valid: true},
	{raw: "$150", want: price.Value{Kind: price.Fixed, Amount: 150}, valid: true},
	{raw: "$200", want: price.Value{Kind: price.Fixed, Amount: 200}, valid: true},

	{raw: "", reason: "empty is what an unset column used to save as"},
	{raw: "$", reason: "the sigil alone"},
	{raw: "$0", reason: "a free session is Free, not zero dollars"},
	{raw: "$00", reason: "leading zero"},
	{raw: "$01", reason: "leading zero"},
	{raw: "$050", reason: "a second spelling of $50"},
	{raw: "$1001", reason: "one over the cap"},
	{raw: "$99999", reason: "far over the cap"},
	{raw: "$+50", reason: "strconv.Atoi would accept the sign"},
	{raw: "$-50", reason: "strconv.Atoi would accept the sign"},
	{raw: "50", reason: "bare digits: what mapPrice's passthrough could emit"},
	{raw: "$50.00", reason: "no decimals"},
	{raw: "$1,000", reason: "no thousands separator"},
	{raw: "$50 / hour", reason: "the free-text shape D3 allowed"},
	{raw: "$100 / hour", reason: "the free-text shape D3 allowed"},
	{raw: "$ 50", reason: "space after the sigil"},
	{raw: " Free", reason: "Parse does not trim"},
	{raw: "Free ", reason: "Parse does not trim"},
	{raw: "Free\n", reason: "a trailing newline is still not Free"},
	{raw: "free", reason: "Parse does not case-fold"},
	{raw: "FREE", reason: "Parse does not case-fold"},
	{raw: "negotiable", reason: "Parse does not case-fold"},
	{raw: "Negotiable price", reason: "the canonical word is the whole value"},
	{raw: "$٥٠", reason: "Arabic-Indic digits are not ASCII digits"},
	{raw: "€50", reason: "one currency only"},
}

func TestParse(t *testing.T) {
	for _, tc := range parseCases {
		t.Run(tc.raw, func(t *testing.T) {
			got, err := price.Parse(tc.raw)

			if !tc.valid {
				assert.ErrorIs(t, err, price.ErrInvalid, "expected %q to be rejected (%s)", tc.raw, tc.reason)
				assert.False(t, price.IsCanonical(tc.raw))
				return
			}

			require.NoError(t, err, "expected %q to parse", tc.raw)
			assert.Equal(t, tc.want, got)
			assert.True(t, price.IsCanonical(tc.raw))
		})
	}
}

// A canonical string survives a Parse/String round trip byte for byte. This is
// what lets the API store what it validated instead of re-rendering it, and
// what will let the Option 3 backfill read the column with Parse and write it
// back with String without touching a single displayed value.
func TestParseStringRoundTrip(t *testing.T) {
	for _, tc := range parseCases {
		if !tc.valid {
			continue
		}
		t.Run(tc.raw, func(t *testing.T) {
			parsed, err := price.Parse(tc.raw)
			require.NoError(t, err)
			assert.Equal(t, tc.raw, parsed.String())
		})
	}
}

// String refuses to render a Value that Parse would not have produced, so a
// caller that built one by hand writes an empty price the database constraint
// rejects rather than a plausible-looking wrong one.
func TestStringRejectsValuesParseCannotProduce(t *testing.T) {
	assert.Equal(t, "", price.Value{}.String(), "the zero Value is not a price")
	assert.Equal(t, "", price.Value{Kind: "gratis"}.String(), "unknown kind")
	assert.Equal(t, "", price.Value{Kind: price.Fixed, Amount: 0}.String(), "below MinAmount")
	assert.Equal(t, "", price.Value{Kind: price.Fixed, Amount: 1001}.String(), "above MaxAmount")
	assert.Equal(t, "", price.Value{Kind: price.Fixed, Amount: -50}.String(), "negative")

	// Amount is ignored for the amount-less kinds rather than leaking into the
	// rendered value: Kind is the discriminator, not Amount == 0.
	assert.Equal(t, "Free", price.Value{Kind: price.Free, Amount: 50}.String())
	assert.Equal(t, "Negotiable", price.Value{Kind: price.Negotiable, Amount: 50}.String())
}

func TestBounds(t *testing.T) {
	assert.Equal(t, 1, price.MinAmount, "$0 must not be storable; a free session is Free")
	assert.Equal(t, 1000, price.MaxAmount)
}

// KindLabel is what the registration / profile-save / admin-update analytics
// events send as their bounded price dimension. Four values, closed set —
// derived from the same table so a grammar change moves it automatically.
func TestKindLabel(t *testing.T) {
	assert.Equal(t, "free", price.KindLabel("Free"))
	assert.Equal(t, "negotiable", price.KindLabel("Negotiable"))
	assert.Equal(t, "fixed", price.KindLabel("$137"))
	assert.Equal(t, "invalid", price.KindLabel("$50 / hour"))
	assert.Equal(t, "invalid", price.KindLabel(""))

	for _, tc := range parseCases {
		label := price.KindLabel(tc.raw)
		if tc.valid {
			assert.Equal(t, string(tc.want.Kind), label, "raw %q", tc.raw)
		} else {
			assert.Equal(t, "invalid", label, "raw %q", tc.raw)
		}
	}
}
