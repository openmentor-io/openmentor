package reviewtoken

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/openmentor-io/openmentor/api/pkg/redact"
)

func TestNewMintsDistinctWellFormedTokens(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 64; i++ {
		raw, hash, err := New()
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		if !WellFormed(raw) {
			t.Fatalf("New produced a token WellFormed rejects (length %d)", len(raw))
		}
		if len(raw) != Len {
			t.Fatalf("token length = %d, want %d", len(raw), Len)
		}
		if hash != Hash(raw) {
			t.Error("New returned a hash that does not match Hash(raw)")
		}
		if !HashWellFormed(hash) {
			t.Error("New returned a hash HashWellFormed rejects")
		}
		if seen[raw] {
			t.Fatal("New repeated a token: the capability would not be unguessable")
		}
		seen[raw] = true
	}
}

// TestHashIsNotReversibleToTheToken is the "hashed at rest" property: what the
// database holds must not contain the value that authorizes.
func TestHashIsNotReversibleToTheToken(t *testing.T) {
	raw, hash, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if strings.Contains(hash, raw) || strings.Contains(hash, strings.TrimPrefix(raw, Prefix)) {
		t.Error("the stored hash contains the raw token")
	}
	want := sha256.Sum256([]byte(raw))
	if hash != hex.EncodeToString(want[:]) {
		t.Error("Hash is not sha256 of the raw token")
	}
}

func TestWellFormedRejectsJunk(t *testing.T) {
	valid, _, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	cases := map[string]string{
		"empty":            "",
		"no prefix":        strings.TrimPrefix(valid, Prefix),
		"wrong prefix":     "abc_" + strings.TrimPrefix(valid, Prefix),
		"truncated":        valid[:len(valid)-1],
		"too long":         valid + "A",
		"not base64url":    Prefix + strings.Repeat("!", EncodedLen),
		"a request uuid":   "11111111-2222-4333-8444-555555555555",
		"looks like a jwt": "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.c2ln",
	}
	for name, value := range cases {
		if WellFormed(value) {
			t.Errorf("WellFormed accepted %s", name)
		}
	}
	// Surrounding whitespace is tolerated: a mail client may wrap the link.
	if !WellFormed("  " + valid + "\n") {
		t.Error("WellFormed rejected a token with surrounding whitespace")
	}
}

// TestHashIgnoresOnlySurroundingWhitespace pins that nothing else is normalized:
// a mangled token must fail closed rather than be repaired into a match.
func TestHashIgnoresOnlySurroundingWhitespace(t *testing.T) {
	raw, hash, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if Hash(" "+raw+"\t") != hash {
		t.Error("Hash is sensitive to surrounding whitespace")
	}
	if Hash(strings.ToUpper(raw)) == hash {
		t.Error("Hash folds case: a mangled token would be repaired into a match")
	}
}

// TestRedactSweepsMintedTokens pins the two definitions of the token's shape
// together. pkg/redact deliberately duplicates the prefix and length so it stays
// a dependency-free leaf; if this package's format changes and redact's copy does
// not, a leaked token stops being scrubbed from log lines and span attributes.
func TestRedactSweepsMintedTokens(t *testing.T) {
	raw, _, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, message := range []string{
		raw,
		"no spendable invitation for " + raw,
		"POST /api/v1/reviews/submit rejected token=" + raw,
		`Post "https://openmentor.io/reviews/new": body carried ` + raw + ", giving up",
	} {
		if got := redact.Text(message); strings.Contains(got, raw) {
			t.Errorf("redact.Text left the token in place: %q", got)
		}
	}
	// Prose that merely mentions the prefix is left alone: over-eager redaction of
	// ordinary log lines is how a redaction rule gets turned off again.
	if got := redact.Text("the rvw_ prefix marks a review capability"); got != "the rvw_ prefix marks a review capability" {
		t.Errorf("redact.Text mangled prose: %q", got)
	}
}
