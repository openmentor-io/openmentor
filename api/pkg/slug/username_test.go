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
