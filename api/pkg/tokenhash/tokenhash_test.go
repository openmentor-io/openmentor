package tokenhash

import "testing"

func TestHashIsStableAndNotTheInput(t *testing.T) {
	const token = "mcf_0123456789abcdef"

	first, second := Hash(token), Hash(token)
	if first != second {
		t.Fatalf("Hash is not deterministic: %q vs %q — a lookup by hash could never match", first, second)
	}
	if first == token {
		t.Fatal("Hash returned its input")
	}
	if len(first) != 64 {
		t.Errorf("len(Hash) = %d, want 64 hex characters of SHA-256", len(first))
	}
	if Hash(token) == Hash(token+"x") {
		t.Error("two different tokens hash to the same value")
	}
}

// TestIsLegacyPlaintextRejectsADigest is the property the prefix scoping exists
// for: without it, "submitted == stored" would let anyone holding a database dump
// present the stored digest as their token, which undoes the hashing entirely.
func TestIsLegacyPlaintextRejectsADigest(t *testing.T) {
	const token = "mcf_0123456789abcdef"

	if !IsLegacyPlaintext(token) {
		t.Errorf("IsLegacyPlaintext(%q) = false: pre-D57 links would stop working", token)
	}
	if IsLegacyPlaintext(Hash(token)) {
		t.Error("a stored digest was classified as a legacy plaintext token")
	}
	for _, other := range []string{"", "mtk_a_login_token", "atk_an_admin_token", "mcf", "MCF_uppercase"} {
		if IsLegacyPlaintext(other) {
			t.Errorf("IsLegacyPlaintext(%q) = true, want false", other)
		}
	}
}

// TestHashMatchesSHA256Vectors pins the derivation itself, not just its
// determinism: the lookup is an indexed equality match against a stored hash,
// so ANY change to the algorithm invalidates every live magic link. Moved from
// internal/repository/token_test.go when HashLoginToken became tokenhash.Hash.
func TestHashMatchesSHA256Vectors(t *testing.T) {
	if got := Hash("foo"); got != "2c26b46b68ffc68ff99b453c1d30413413422d706483bfa0f98a5e886266e7ae" {
		t.Errorf("Hash(\"foo\") = %q: the derivation changed — every live magic link is now invalid", got)
	}
	if got := Hash(""); got != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Errorf("Hash(\"\") = %q, want the SHA-256 empty-string digest", got)
	}
}
