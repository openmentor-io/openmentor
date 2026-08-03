package redact

import (
	"net/url"
	"strings"
	"testing"
)

// sentinel is the value the tests follow through every helper. It is shaped
// like a real request id so nothing can pass by rejecting the format.
const sentinel = "11111111-2222-4333-8444-555555555555"

func TestSensitiveKeyMatchesEverySpelling(t *testing.T) {
	sensitive := []string{
		"request_id", "requestId", "REQUEST-ID", "client_request_id",
		"token", "login_token", "confirm_token", "captchaToken", "access_token",
		"api_key", "apikey", "X-Auth-Token", "session", "cookie", "otp",
		"password", "client_secret", "signature",
		// Substring matching over-redacts rather than under-redacts: a derived
		// value like this is not a secret, but a log line is not worth the risk.
		"captcha_token_length",
	}
	for _, key := range sensitive {
		if !SensitiveKey(key) {
			t.Errorf("SensitiveKey(%q) = false, want true", key)
		}
	}

	safe := []string{"id", "slug", "status", "group", "u", "page", "mentor_id", "review_id", ""}
	for _, key := range safe {
		if SensitiveKey(key) {
			t.Errorf("SensitiveKey(%q) = true, want false", key)
		}
	}
}

func TestQueryRedactsSensitiveValues(t *testing.T) {
	values := url.Values{
		"request_id":  []string{sentinel},
		"login_token": []string{sentinel},
		"status":      []string{"done"},
	}

	redacted := Query(values)

	if redacted["status"] != "done" {
		t.Errorf("status = %q, want it kept", redacted["status"])
	}
	for _, key := range []string{"request_id", "login_token"} {
		if redacted[key] != Placeholder {
			t.Errorf("%s = %q, want %q", key, redacted[key], Placeholder)
		}
	}
	assertNoSentinel(t, redacted)
}

func TestQueryStringRedactsSensitiveValues(t *testing.T) {
	cases := map[string]string{
		"request_id=" + sentinel:                  "request_id=" + Placeholder,
		"requestId=" + sentinel + "&status=done":  "requestId=" + Placeholder + "&status=done",
		"request%5Fid=" + sentinel:                "request%5Fid=" + Placeholder,
		"token=" + url.QueryEscape("a.b.c/d+e=="): "token=" + Placeholder,
		"status=done":                             "status=done",
	}

	for raw, want := range cases {
		if got := QueryString(raw); got != want {
			t.Errorf("QueryString(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestIDIsNotReversibleAndIsStable(t *testing.T) {
	ref := ID(sentinel)

	if ref == "" {
		t.Fatal("ID returned an empty reference for a non-empty value")
	}
	if strings.Contains(ref, sentinel) {
		t.Errorf("ID(%q) = %q, which still carries the value", sentinel, ref)
	}
	if ID(sentinel) != ref {
		t.Error("ID is not stable for the same input")
	}
	if ID("11111111-2222-4333-8444-555555555556") == ref {
		t.Error("ID collides across different inputs")
	}
	if ID("  ") != "" {
		t.Error("ID of a blank value should be empty, not a hash of nothing")
	}
}

func assertNoSentinel(t *testing.T, values map[string]string) {
	t.Helper()
	for key, value := range values {
		if strings.Contains(value, sentinel) {
			t.Errorf("%s still carries the sentinel: %q", key, value)
		}
	}
}
