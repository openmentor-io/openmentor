// Package reviewtoken mints and hashes the capability that authorizes one
// review submission (H4).
//
// It replaces client_requests.id, which used to be the capability: the primary
// key traveled in an email URL, in every request log line, in span attributes
// and in analytics properties, and holding it was sufficient to submit a review
// attributed to that mentee/mentor pair. A capability has to be a value that is
// safe to burn, and a primary key is not.
//
// The token is minted by the worker when it sends the "session complete" email
// and verified by the API. Only sha256(token) is ever stored, so the raw value
// exists in exactly two places: the email that carried it and the request body
// that spends it.
package reviewtoken

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// Prefix marks the value as a review capability wherever it turns up. It is not
// a secret; it exists so a leaked token is greppable and recognizable by secret
// scanners, and so redact.Text can strip it out of free-form text by shape.
const Prefix = "rvw_"

// entropyBytes is the raw random length. 32 bytes / 256 bits is the floor the
// audit set: the token has to be unguessable on its own, because unlike the
// request id it is the ONLY thing standing between a caller and a submission.
const entropyBytes = 32

// EncodedLen is the length of the base64url body after Prefix.
const EncodedLen = 43 // base64.RawURLEncoding.EncodedLen(32)

// Len is the total length of a well-formed raw token.
const Len = len(Prefix) + EncodedLen

// hashLen is the length of the lowercase-hex sha256 digest stored in
// review_invitations.token_hash (pinned by a CHECK constraint in migration
// 000011).
const hashLen = sha256.Size * 2

// TTL is how long an invitation stays spendable. Thirty days is long enough
// that a mentee who reads the mail on holiday can still answer, and short enough
// that a link leaked through a forwarded mail or a shared screenshot stops being
// a capability within a month. Deliberately a constant rather than an env var:
// there is no operational reason to tune it per environment, and every knob in
// the env contract is a knob that can be misconfigured.
const TTL = 30 * 24 * time.Hour

// New mints a fresh capability. It returns the raw token, which goes in exactly
// one place (the email), and its hash, which is the only form persisted.
//
// base64url with no padding keeps the value safe in a URL fragment without
// percent-encoding, which matters because the fragment is how the token reaches
// the browser: a fragment is never sent to a server, so it cannot land in an
// access log, a span's url.path or PostHog's $current_url the way the old query
// parameter did.
func New() (raw, hash string, err error) {
	buf := make([]byte, entropyBytes)
	if _, err = rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("failed to generate review token: %w", err)
	}
	raw = Prefix + base64.RawURLEncoding.EncodeToString(buf)
	return raw, Hash(raw), nil
}

// Hash returns the lookup digest for a raw token. sha256 with no salt or
// stretching is correct here and bcrypt/argon2 would not be: the input is 256
// bits of uniform randomness, so there is nothing to brute-force and no low
// entropy to compensate for, and the digest has to be a deterministic index key.
//
// The input is trimmed but NOT otherwise normalized: a mail client that mangles
// the case of the token must fail closed rather than be repaired into a match.
func Hash(raw string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(raw)))
	return hex.EncodeToString(sum[:])
}

// WellFormed reports whether a submitted value has the shape of a token this
// package mints. It is a cheap pre-filter so junk never reaches the database,
// NOT an authorization check — a well-formed token still has to match a live,
// unconsumed, unexpired invitation.
func WellFormed(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if len(trimmed) != Len || !strings.HasPrefix(trimmed, Prefix) {
		return false
	}
	_, err := base64.RawURLEncoding.DecodeString(trimmed[len(Prefix):])
	return err == nil
}

// HashWellFormed reports whether a value is a digest this package produced. Used
// by the repository to keep a raw token from ever being passed where a hash is
// expected — the CHECK constraint in migration 000011 is the same guard in SQL.
func HashWellFormed(hash string) bool {
	if len(hash) != hashLen {
		return false
	}
	_, err := hex.DecodeString(hash)
	return err == nil
}
