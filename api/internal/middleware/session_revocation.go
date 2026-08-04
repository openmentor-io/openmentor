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
