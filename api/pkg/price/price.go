// Package price is the one definition of what a mentor price may be.
//
// The stored form is a closed grammar — "Free", "Negotiable", or "$N" for a
// whole dollar amount between MinAmount and MaxAmount — but a price is modeled
// here as a {Kind, Amount} pair rather than as a string with a regexp over it.
// Every reader and writer goes through Parse and Value.String, so moving the
// column to a numeric price_amount + price_kind later moves this package's
// boundary instead of rewriting its callers (D87).
//
// Parse is deliberately STRICT: it accepts the canonical spelling and nothing
// else, no trimming and no case folding. The web form serializes from the same
// {kind, amount} pair (web/src/lib/price.ts), so a value that needs cleaning up
// is a bug in a caller, and a lenient parser here would hide it and let a
// second spelling of "$50" into a column the catalog filters have to classify.
package price

import (
	"errors"
	"strconv"
)

// Kind is which of the three prices a mentor has chosen. Fixed is the only one
// that carries an Amount.
type Kind string

const (
	Free       Kind = "free"
	Negotiable Kind = "negotiable"
	Fixed      Kind = "fixed"
)

// The inclusive bounds on a Fixed amount. MinAmount is 1, not 0: a mentor who
// charges nothing has Kind Free, which is a different statement from charging
// zero dollars, and the mentors_price_chk constraint enforces the same floor.
const (
	MinAmount = 1
	MaxAmount = 1000
)

// The canonical spellings of the two amount-less kinds.
const (
	freeText       = "Free"
	negotiableText = "Negotiable"
)

// ErrInvalid is returned by Parse for anything outside the grammar. It carries
// no copy of the offending input: this error reaches a 400 whose body the
// caller already knows, and errors in this codebase end up in log lines.
var ErrInvalid = errors.New("price: not a canonical price")

// Value is a parsed price. The zero Value is not a valid price.
type Value struct {
	Kind Kind
	// Amount is meaningful only when Kind is Fixed, and is zero otherwise.
	// Read Kind first; do not treat Amount == 0 as "free".
	Amount int
}

// Parse accepts exactly "Free", "Negotiable" or "$N" and returns the value it
// denotes.
func Parse(s string) (Value, error) {
	switch s {
	case freeText:
		return Value{Kind: Free}, nil
	case negotiableText:
		return Value{Kind: Negotiable}, nil
	}

	if len(s) < 2 || s[0] != '$' {
		return Value{}, ErrInvalid
	}
	digits := s[1:]

	// A leading zero would give "$50" a second spelling ("$050"), and "$0" is
	// below MinAmount anyway. Rejecting the prefix covers both.
	if digits[0] == '0' {
		return Value{}, ErrInvalid
	}
	// strconv.Atoi accepts a leading sign, which "$+50" would smuggle through.
	for i := 0; i < len(digits); i++ {
		if digits[i] < '0' || digits[i] > '9' {
			return Value{}, ErrInvalid
		}
	}

	amount, err := strconv.Atoi(digits)
	if err != nil || amount < MinAmount || amount > MaxAmount {
		return Value{}, ErrInvalid
	}
	return Value{Kind: Fixed, Amount: amount}, nil
}

// IsCanonical reports whether s is a price this package would store verbatim.
func IsCanonical(s string) bool {
	_, err := Parse(s)
	return err == nil
}

// KindLabel is the Kind of a stored price as a bounded analytics dimension:
// "free", "negotiable", "fixed", or "invalid" for anything Parse rejects.
// Four values, closed set — safe as an event property where the raw price
// (~1002 values) or arbitrary legacy text would rot every breakdown.
func KindLabel(s string) string {
	v, err := Parse(s)
	if err != nil {
		return "invalid"
	}
	return string(v.Kind)
}

// String renders the canonical stored spelling. It returns "" for a Value whose
// Kind is unset, so a caller that skipped Parse writes an empty price the
// database constraint rejects rather than a plausible-looking wrong one.
func (v Value) String() string {
	switch v.Kind {
	case Free:
		return freeText
	case Negotiable:
		return negotiableText
	case Fixed:
		if v.Amount < MinAmount || v.Amount > MaxAmount {
			return ""
		}
		return "$" + strconv.Itoa(v.Amount)
	default:
		return ""
	}
}
