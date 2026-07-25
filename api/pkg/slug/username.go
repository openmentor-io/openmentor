package slug

import (
	"errors"
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

// NormalizeUsername canonicalizes user input before validation/storage:
// trim + lowercase. It deliberately does NOT transliterate or strip — a
// username that needs fixing up should be rejected, not silently rewritten.
func NormalizeUsername(input string) string {
	return strings.ToLower(strings.TrimSpace(input))
}

// ValidateUsername checks a NORMALIZED username against format, length and
// the reserved list. Returns one of the Err* sentinel errors, or nil.
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
	return nil
}
