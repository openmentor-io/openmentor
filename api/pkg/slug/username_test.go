package slug

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeUsername(t *testing.T) {
	cases := map[string]string{
		"  Jane-Doe  ": "jane-doe",
		"IVAN":         "ivan",
		"already-ok":   "already-ok",
		"\tmixed Case": "mixed case", // invalid, but normalization only trims+lowers
	}
	for input, want := range cases {
		if got := NormalizeUsername(input); got != want {
			t.Errorf("NormalizeUsername(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestValidateUsername_Valid(t *testing.T) {
	for _, u := range []string{
		"jane", "jane-doe", "j4ne", "123", "a-b-c", "abc",
		strings.Repeat("a", UsernameMaxLength),
	} {
		if err := ValidateUsername(u); err != nil {
			t.Errorf("ValidateUsername(%q) = %v, want nil", u, err)
		}
	}
}

func TestValidateUsername_Invalid(t *testing.T) {
	cases := map[string]error{
		"ab":                                     ErrUsernameTooShort,
		"":                                       ErrUsernameTooShort,
		strings.Repeat("a", UsernameMaxLength+1): ErrUsernameTooLong,
		"Jane":                                   ErrUsernameBadFormat, // uppercase — must be normalized first
		"jane doe":                               ErrUsernameBadFormat,
		"-jane":                                  ErrUsernameBadFormat,
		"jane-":                                  ErrUsernameBadFormat,
		"jane--doe":                              ErrUsernameBadFormat,
		"jane.doe":                               ErrUsernameBadFormat,
		"иван":                                   ErrUsernameBadFormat,
		"jane_doe":                               ErrUsernameBadFormat,
		"login":                                  ErrUsernameReserved,
		"admin":                                  ErrUsernameReserved,
		"openmentor":                             ErrUsernameReserved,
		"profile":                                ErrUsernameReserved,
	}
	for input, want := range cases {
		if err := ValidateUsername(input); !errors.Is(err, want) {
			t.Errorf("ValidateUsername(%q) = %v, want %v", input, err, want)
		}
	}
}

// "mentor" and everything built from it is rejected — the stem may appear
// anywhere in the username, and hyphen/digit spellings fold onto it first.
func TestValidateUsername_MentorDerivatives(t *testing.T) {
	for _, u := range []string{
		// The bare stem and its inflections.
		"mentor", "mentors", "mentoring", "mentorship", "mentored", "mentorka",
		// Stem as prefix, suffix and infix.
		"mentor-anna", "anna-mentor", "topmentor", "the-best-mentor", "mentor1",
		// Brand-ish compounds (also on the reserved list; still derivatives).
		"openmentor", "getmentor",
		// Separator and digit-substitution spellings.
		"me-ntor", "m-e-n-t-o-r", "m3nt0r", "men7or", "m3nt0rship", "ment0r-anna",
	} {
		err := ValidateUsername(u)
		if !errors.Is(err, ErrUsernameReserved) {
			t.Errorf("ValidateUsername(%q) = %v, want it to classify as reserved", u, err)
		}
	}
}

// The stem rule must not swallow names that merely share letters with it.
func TestValidateUsername_NotMentorDerivatives(t *testing.T) {
	for _, u := range []string{
		"anna-smith", "ment", "mento", "menter", "torment", "mentha", "normen",
		"nemtor", "mentr", "m3nt", "elena-mentz",
	} {
		if err := ValidateUsername(u); err != nil {
			t.Errorf("ValidateUsername(%q) = %v, want nil", u, err)
		}
	}
}

// The wrapped sentinel is what carries the specific message; handlers and the
// availability endpoint still see ErrUsernameReserved (asserted above).
func TestValidateUsername_MentorDerivativeSentinel(t *testing.T) {
	err := ValidateUsername("anna-mentor")
	if !errors.Is(err, ErrUsernameMentorDerivative) {
		t.Errorf("ValidateUsername(%q) = %v, want ErrUsernameMentorDerivative", "anna-mentor", err)
	}
	if err != nil && !strings.Contains(err.Error(), "mentor") {
		t.Errorf("error message %q should name the offending word", err.Error())
	}
}
