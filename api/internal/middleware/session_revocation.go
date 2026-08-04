package middleware

import "net/http"

// sessionVersionCurrent compares the session_version a token was minted with
// against the identity row's current value (D58).
//
// A claim of 0 means the token predates the claim — those are grandfathered in
// rather than rejected, because rejecting them would log out every live session
// the moment this deploys. Delete this allowance one session TTL (24h) after the
// rollout, at which point no token without the claim can still be valid.
func sessionVersionCurrent(claimed, current int) bool {
	if claimed == 0 {
		return true
	}
	return claimed == current
}

// LiveCheckScope selects which requests re-read the identity row behind a
// session. It is a required argument rather than a default so that the scope
// travels with the route group in the router, where the routes are visible:
// deciding it from a path string kept somewhere else means renaming a route
// silently downgrades its authorization.
type LiveCheckScope int

const (
	// LiveCheckOnMutations re-reads on state-changing methods only. Correct for
	// routes whose responses contain nothing but the session holder's OWN data,
	// where a stale cookie discloses only what its holder already has:
	//   GET /api/v1/auth/mentor/session — the token's own claims, no query at all
	//   GET /api/v1/mentor/profile      — the mentor's own row
	//   GET /api/v1/mentor/username     — the mentor's own username state
	//   GET /api/v1/mentor/username/availability — is a name free; nobody's data
	// These are also the per-page-load reads, so exempting them is what keeps the
	// portal off a database round trip per navigation.
	LiveCheckOnMutations LiveCheckScope = iota

	// LiveCheckOnEveryRequest re-reads on reads too. Required for routes that
	// return THIRD-PARTY personal data, where a revoked cookie staying readable
	// for the rest of its 24h is the actual disclosure:
	//   GET /api/v1/mentor/requests     — mentee name, email, preferred contact
	//   GET /api/v1/mentor/requests/:id — the same, plus the message body
	// The extra primary-key lookup is free in practice on exactly these routes:
	// they already query the database, so a re-read cannot fail on a request that
	// would otherwise have succeeded.
	LiveCheckOnEveryRequest
)

// isMutation reports whether a request method changes state. GET/HEAD/OPTIONS are
// the safe methods; everything else is treated as a mutation, so a method added
// to a route later is checked by default rather than by remembering to add it.
func isMutation(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	default:
		return true
	}
}
