package slug

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Username (custom slug) validation. Publicly the field is called "username";
// internally it stays the mentors.slug column — same value, one identity.

const (
	// UsernameMinLength / UsernameMaxLength bound the accepted length.
	UsernameMinLength = 3
	UsernameMaxLength = 40
)

// Typed validation errors — handlers map these to structured 422 responses.
var (
	ErrUsernameTooShort  = errors.New("username must be at least 3 characters")
	ErrUsernameTooLong   = errors.New("username must be at most 40 characters")
	ErrUsernameBadFormat = errors.New("username may contain lowercase letters, digits and single hyphens, and must start and end with a letter or digit")
	ErrUsernameReserved  = errors.New("this username is reserved")
)

// ErrUsernameMentorDerivative rejects "mentor" and anything built from it. It
// WRAPS ErrUsernameReserved on purpose: the handler mapping, the availability
// endpoint's "reserved" reason and the frontend copy all key off that sentinel,
// so the rule needs no new branch anywhere — only the message is specific.
var ErrUsernameMentorDerivative = fmt.Errorf(`%w: "mentor" and words built from it are not allowed in a username`, ErrUsernameReserved)

// usernameRe: lowercase alphanumeric groups separated by single hyphens; no
// leading/trailing hyphen, no consecutive hyphens.
var usernameRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// reservedUsernames blocks values that collide with site routes (everything
// that lives, or may live, under /mentor/* is the hard requirement — a mentor
// named "login" would be shadowed by the static route) plus brand/staff-ish
// words that would be misleading on a public profile URL.
var reservedUsernames = map[string]bool{
	// /mentor/* static routes (Next.js static routes win over [slug]).
	"login": true, "logout": true, "confirm": true, "past": true,
	"profile": true, "requests": true, "auth": true, "session": true,
	// Site routes and obvious namespace words.
	"admin": true, "api": true, "about": true, "faq": true, "donate": true,
	"migrate": true, "privacy": true, "terms": true, "bementor": true,
	"register": true, "review": true, "reviews": true, "sitemap": true,
	"search": true, "settings": true, "help": true, "support": true,
	"mentor": true, "mentors": true, "mentee": true, "mentees": true,
	// Generic path segments that read as UI, not a person.
	"new": true, "edit": true, "me": true, "static": true, "assets": true,
	"images": true, "img": true, "index": true, "null": true, "undefined": true,
	// Brand / staff impersonation.
	"openmentor": true, "getmentor": true, "official": true, "moderator": true,
	"moderators": true, "administrator": true, "team": true, "staff": true,
	"root": true, "system": true,
}

// mentorStemFold collapses the spellings that still read as "mentor": hyphens
// dropped ("me-ntor") and the digits people substitute for letters folded back
// ("m3nt0r", "men7or"). Only substitutions that can actually spell the stem are
// listed — a wider fold buys nothing and widens the false-positive surface.
var mentorStemFold = strings.NewReplacer(
	"-", "",
	"0", "o",
	"3", "e",
	"7", "t",
)

// isMentorDerivative reports whether a normalized username is built from
// "mentor". The stem is matched ANYWHERE in the value, so "mentors",
// "mentoring", "anna-mentor" and "topmentor" are all rejected: on a mentorship
// platform each one reads as an official /mentor/* URL rather than as a person.
// The deliberate cost is that an incidental substring ("tormentor",
// "elementor") is rejected too — the alternative is an exception list nobody
// can keep complete.
func isMentorDerivative(username string) bool {
	return strings.Contains(mentorStemFold.Replace(username), "mentor")
}

// NormalizeUsername canonicalizes user input before validation/storage:
// trim + lowercase. It deliberately does NOT transliterate or strip — a
// username that needs fixing up should be rejected, not silently rewritten.
func NormalizeUsername(input string) string {
	return strings.ToLower(strings.TrimSpace(input))
}

// ValidateUsername checks a NORMALIZED username against format, length, the
// reserved list and the "mentor" stem rule. Returns one of the Err* sentinel
// errors, or nil.
func ValidateUsername(username string) error {
	if len(username) < UsernameMinLength {
		return ErrUsernameTooShort
	}
	if len(username) > UsernameMaxLength {
		return ErrUsernameTooLong
	}
	if !usernameRe.MatchString(username) {
		return ErrUsernameBadFormat
	}
	if reservedUsernames[username] {
		return ErrUsernameReserved
	}
	if isMentorDerivative(username) {
		return ErrUsernameMentorDerivative
	}
	return nil
}
