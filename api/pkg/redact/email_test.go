package redact

import (
	"strings"
	"testing"
)

// menteeAddress is the sentinel every test in this file follows. Deliberately in
// example.com (RFC 2606) so no test, CI log or failure message ever carries an
// address that could belong to a person.
const menteeAddress = "mentee.person+tag@example.com"

func TestEmailKeepsTheDomainAndNothingElse(t *testing.T) {
	cases := []struct{ in, want string }{
		{menteeAddress, "m***@example.com"},
		{"MENTEE@Example.COM", "M***@Example.COM"},
		// Trimmed, because a value read out of a form or a header has whitespace.
		{"  mentee@example.com  ", "m***@example.com"},
		// A single-character local part has nothing to keep: revealing it would
		// be revealing the whole local part.
		{"a@example.com", "*@example.com"},
		// Not addresses. Fully masked rather than echoed: the input could be
		// anything, up to and including a token.
		{"not-an-address", EmailMask},
		{"@example.com", EmailMask},
		{"mentee@", EmailMask},
		{"", ""},
		{"   ", ""},
	}
	for _, tc := range cases {
		if got := Email(tc.in); got != tc.want {
			t.Errorf("Email(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestEmailIsIdempotent matters because the same value can pass the masker twice
// — once at the call site, once in the logger's redacting core.
func TestEmailIsIdempotent(t *testing.T) {
	once := Email(menteeAddress)
	if twice := Email(once); twice != once {
		t.Errorf("Email(Email(x)) = %q, want %q", twice, once)
	}
	if twice := Text(once); twice != once {
		t.Errorf("Text(Email(x)) = %q, want %q", twice, once)
	}
}

// TestEmailIsStablePerAddress is the debuggability half of the C11 trade: an
// operator has to be able to tell "this recipient failed then succeeded" from
// two log lines, which a masked address supports and a dropped field does not.
func TestEmailIsStablePerAddress(t *testing.T) {
	first := Email(menteeAddress)
	second := Email(" " + menteeAddress + " ")
	if first != second {
		t.Errorf("Email is not stable for the same address: %q vs %q", first, second)
	}
	if Email("mentee@example.com") == Email("mentee@example.org") {
		t.Error("Email lost the domain, which is the field's whole debugging value")
	}
}

// TestTextMasksEmbeddedAddresses is the case that carries the fix: the addresses
// that reach logs are mostly not in fields anyone wrote by hand, they are inside
// error strings produced by SES and Postgres.
func TestTextMasksEmbeddedAddresses(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{
			"ses rejection quoting the destination",
			"MessageRejected: the following identities failed the check in region eu-central-1: " + menteeAddress,
			"MessageRejected: the following identities failed the check in region eu-central-1: m***@example.com",
		},
		{
			"postgres unique violation DETAIL",
			`ERROR: duplicate key value violates unique constraint (SQLSTATE 23505) Key (email)=(mentee@example.com) already exists.`,
			`ERROR: duplicate key value violates unique constraint (SQLSTATE 23505) Key (email)=(m***@example.com) already exists.`,
		},
		{
			// The URL rules run first and keep this value, because "email" is not
			// a capability key — so the email sweep has to run after them.
			"address surviving inside a query string",
			`Get "https://api.example.com/hook?email=mentee@example.com": dial tcp: timeout`,
			`Get "https://api.example.com/hook?email=m***@example.com": dial tcp: timeout`,
		},
		{
			"trailing sentence punctuation is not part of the domain",
			"could not reach mentee@example.com.",
			"could not reach m***@example.com.",
		},
		{
			"two addresses in one message",
			"reroute mentee@example.com to dev@example.org",
			"reroute m***@example.com to d***@example.org",
		},
		{
			// Adjacent @s: the second @'s "local part" is the first match's
			// domain. This shape arrives from outside (a User-Agent header, a
			// mailto: URI in error text, a double-pasted address) and used to
			// slice sweepEmails out of bounds — a panic on the write path of
			// every log line.
			"address immediately followed by a second at-domain",
			"see mailto:john@example.com@evil.example.org for details",
			"see mailto:j***@example.com@evil.example.org for details",
		},
		{
			"double-pasted address",
			"login failed for x@a.com@a.com",
			"login failed for *@a.com@a.com",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Text(tc.in)
			if got != tc.want {
				t.Errorf("Text() = %q, want %q", got, tc.want)
			}
			if strings.Contains(got, "mentee.person") || strings.Contains(got, "mentee@") {
				t.Errorf("Text() still carries a local part: %q", got)
			}
		})
	}
}

// TestTextLeavesNonAddressAtSignsAlone pins the false positives that would make
// this sweep unusable — every one of these shapes appears in real error text.
func TestTextLeavesNonAddressAtSignsAlone(t *testing.T) {
	unchanged := []string{
		// Go module path: the "TLD" is a version number.
		"github.com/openmentor-io/openmentor@v1.2.3 does not exist",
		// Digest-pinned image: nothing after the @ has a dot.
		"pulling grafana/alloy@sha256:deadbeefdeadbeef",
		// No local part.
		"stray @ sign in the middle",
		// Single-letter TLD.
		"user@host.x",
	}
	for _, in := range unchanged {
		if got := Text(in); got != in {
			t.Errorf("Text(%q) = %q, want it untouched", in, got)
		}
	}
}

func TestDSNKeepsTheRoleAndDropsThePassword(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{
			"full url dsn",
			"postgres://om_migrate:sup3rs3cret@db.internal:5432/openmentor?sslmode=require",
			"postgres://om_migrate:***@db.internal:5432/openmentor?sslmode=require",
		},
		{
			// The regression the old byte-prefix mask hid: a short username moves
			// the password inside the first 20 bytes.
			"short username",
			"postgres://om:sup3rs3cret@db.internal:5432/openmentor",
			"postgres://om:***@db.internal:5432/openmentor",
		},
		{
			"no password at all",
			"postgres://om_migrate@db.internal:5432/openmentor",
			"postgres://om_migrate@db.internal:5432/openmentor",
		},
		{
			"password also passed as a query parameter",
			"postgres://om:pw@db.internal/openmentor?sslmode=require&password=pw",
			"postgres://om:***@db.internal/openmentor?sslmode=require&password=" + Placeholder,
		},
		{
			"libpq keyword/value dsn is not a url",
			"host=db.internal user=om password=sup3rs3cret dbname=openmentor",
			"host=db.internal user=om password=" + Placeholder + " dbname=openmentor",
		},
		{
			// Too malformed for net/url to give a host, but still carrying a real
			// password — the fallback has to mask it, not echo it.
			"malformed dsn",
			"postgres:/om:sup3rs3cret@db.internal/openmentor",
			"postgres:/om:***@db.internal/openmentor",
		},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DSN(tc.in)
			if got != tc.want {
				t.Errorf("DSN() = %q, want %q", got, tc.want)
			}
			if strings.Contains(got, "sup3rs3cret") {
				t.Errorf("DSN() leaked the password: %q", got)
			}
		})
	}
}

// TestDSNBeatsTheMaskItReplaced is the "fails without the fix" proof for C11's
// named finding. The old mask was url[:20] + "***"; this asserts that mask leaks
// on a short username and that DSN does not.
func TestDSNBeatsTheMaskItReplaced(t *testing.T) {
	const dsn = "postgres://om:sup3rs3cret@db.internal/openmentor"

	oldMask := dsn[:20] + "***"
	if !strings.Contains(oldMask, "sup3rs") {
		t.Fatalf("the byte-prefix mask no longer leaks on this input (%q); the regression case needs updating", oldMask)
	}
	if got := DSN(dsn); strings.Contains(got, "sup3rs") {
		t.Errorf("DSN() leaks the same password bytes the old mask did: %q", got)
	}
}

// TestTextStripsDSNPasswordsInErrorText covers the sink DSN itself cannot reach:
// a driver that fails to connect renders the whole connection string into its
// error message.
func TestTextStripsDSNPasswordsInErrorText(t *testing.T) {
	in := `failed to connect: cannot parse "postgres://om:sup3rs3cret@db.internal:5432/openmentor": invalid port`
	got := Text(in)

	if strings.Contains(got, "sup3rs3cret") {
		t.Errorf("Text() left a DSN password in place: %q", got)
	}
	if !strings.Contains(got, "postgres://om:***@db.internal:5432/openmentor") {
		t.Errorf("Text() mangled the rest of the DSN, which is what makes the line useful: %q", got)
	}
}
