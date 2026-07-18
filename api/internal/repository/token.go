package repository

import (
	"crypto/sha256"
	"encoding/hex"
)

// HashLoginToken returns the hex-encoded SHA-256 of a login/one-time token.
// SECURITY: login tokens are stored and looked up by this hash, never in
// plaintext, so a database dump/backup can't be replayed as a valid credential
// (L1). Tokens are high-entropy (256-bit / UUIDv4), so an unsalted hash is
// sufficient and keeps lookups a simple indexed equality match.
func HashLoginToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
