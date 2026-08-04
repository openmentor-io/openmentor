// Package tokenhash is the single definition of how a one-time credential is
// stored at rest.
//
// SECURITY: magic-link login tokens and email-confirmation tokens are stored
// and looked up by this hash, never in plaintext, so a database dump or backup
// cannot be replayed as a valid credential (L1). Tokens are high-entropy
// (256-bit random), so an unsalted hash is sufficient and keeps lookups a plain
// indexed equality match.
//
// It lives in pkg/ rather than internal/repository because the API mints
// confirmation tokens in internal/services and the worker mints them in
// internal/worker, and both must agree with the repository on the stored form.
// Two copies of a sha256 call is exactly how one side ends up unable to match
// the other's rows.
package tokenhash

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// legacyConfirmationPrefix is the prefix every PLAINTEXT email-confirmation
// token carries ("mcf_" + 64 hex chars). Login tokens are "mtk_"/"atk_".
//
// Hashes are bare 64-char hex with no prefix, which is what makes this a safe
// discriminator: see IsLegacyPlaintext.
const legacyConfirmationPrefix = "mcf_"

// Hash returns the hex-encoded SHA-256 of a one-time token.
func Hash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// IsLegacyPlaintext reports whether a submitted confirmation token looks like
// one that was issued BEFORE confirmation tokens were hashed at rest (D57), and
// therefore still sits in the database verbatim.
//
// The prefix test is load-bearing, not cosmetic. Accepting "submitted == stored"
// unconditionally as a fallback would undo the hashing entirely: an attacker
// holding a database dump could submit the stored HASH as their token and match
// the row through the plaintext branch. A stored hash never carries the prefix,
// so scoping the fallback to prefixed values closes that.
//
// Remove this (and its two call sites) once no unhashed confirmation token can
// still be outstanding — they live 24h, so one day after the deploy.
func IsLegacyPlaintext(token string) bool {
	return strings.HasPrefix(token, legacyConfirmationPrefix)
}
