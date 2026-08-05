package repository

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// HashLoginToken is the only thing standing between a database backup and a
// replayable credential (L1), and it needs no database to test.
func TestHashLoginTokenNeverReturnsThePlaintext(t *testing.T) {
	const token = "3f2504e0-4f89-41d3-9a0c-0305e82c3301"

	hash := HashLoginToken(token)
	require.Len(t, hash, 64, "hex-encoded SHA-256")
	assert.NotContains(t, hash, token)
	assert.False(t, strings.Contains(token, hash))
}

// TestHashLoginTokenIsStable: the lookup is an indexed equality match against a
// stored hash, so any change to the derivation invalidates every live magic link.
func TestHashLoginTokenIsStable(t *testing.T) {
	assert.Equal(t,
		"2c26b46b68ffc68ff99b453c1d30413413422d706483bfa0f98a5e886266e7ae",
		HashLoginToken("foo"))
	assert.Equal(t,
		"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		HashLoginToken(""))
}

func TestHashLoginTokenSeparatesDistinctTokens(t *testing.T) {
	// Off by one byte: the hashes must share nothing.
	a := HashLoginToken("3f2504e0-4f89-41d3-9a0c-0305e82c3301")
	b := HashLoginToken("3f2504e0-4f89-41d3-9a0c-0305e82c3302")
	assert.NotEqual(t, a, b)
}
