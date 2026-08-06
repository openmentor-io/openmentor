package repository

import "github.com/openmentor-io/openmentor/api/pkg/tokenhash"

// HashOneTimeToken returns the stored form of a login or email-confirmation
// token. It is a thin alias so repository callers do not each reach for a hash
// function; pkg/tokenhash documents why the hashing matters (L1) and is shared
// with the worker, which mints confirmation tokens of its own.
//
// Named "one-time" rather than "login" because confirmation tokens go through it
// too since D57.
func HashOneTimeToken(token string) string {
	return tokenhash.Hash(token)
}
