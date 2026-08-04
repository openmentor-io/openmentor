package models

import "errors"

// ErrSessionSubjectGone reports that the identity behind a live session no
// longer exists, as distinct from "the database could not answer". The
// repository returns it and the session middleware branches on it, so it lives
// here — the one package both already import — rather than being redeclared on
// each side, where errors.Is would silently never match.
var ErrSessionSubjectGone = errors.New("session subject no longer exists")

// Mentor statuses that may hold a portal session. Draft and pending mentors need
// the portal to finish or fix their profile; inactive mentors manage their own
// visibility from it. Only 'declined' is shut out.
//
// This is the same rule the magic-link login applies at mint time; the session
// middleware re-applies it per mutation so a mentor declined mid-session stops
// being able to change anything, instead of keeping write access for the rest of
// the token's 24 hours.
func MayHoldMentorSession(status string) bool {
	switch status {
	// Literals rather than constants because the status vocabulary lives in the
	// mentors_status_chk CHECK constraint (migration 000005), and the services
	// package keeps its own unexported copies.
	case "draft", "pending", "active", "inactive":
		return true
	default:
		return false
	}
}
