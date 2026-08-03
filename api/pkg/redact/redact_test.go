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
		// Whole-key match: "key" as a substring would also hit keyword and monkey.
		"key", "KEY",
	}
	for _, key := range sensitive {
		if !SensitiveKey(key) {
			t.Errorf("SensitiveKey(%q) = false, want true", key)
		}
	}

	safe := []string{
		"id", "slug", "status", "group", "u", "page", "mentor_id", "review_id", "",
		"keywords", "monkey",
	}
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

// TestPathRedactsCapabilitySegments pins the value-shape rule: the parameter
// name says nothing, because /api/v1/mentor/requests/<id> carries the same
// client_requests.id that /api/v1/reviews/<id> authorizes on.
func TestPathRedactsCapabilitySegments(t *testing.T) {
	cases := map[string]string{
		"/api/v1/mentor/requests/" + sentinel:             "/api/v1/mentor/requests/" + Placeholder,
		"/api/v1/mentor/requests/" + sentinel + "/status": "/api/v1/mentor/requests/" + Placeholder + "/status",
		"/api/v1/reviews/" + sentinel + "/check":          "/api/v1/reviews/" + Placeholder + "/check",
		"/api/v1/admin/mentors/" + sentinel + "/requests/" + sentinel: "/api/v1/admin/mentors/" + Placeholder +
			"/requests/" + Placeholder,
		// Not UUID-shaped, so still readable: slugs and legacy ids are not capabilities.
		"/api/v1/mentors/jane-doe":     "/api/v1/mentors/jane-doe",
		"/api/v1/mentor/requests/m-42": "/api/v1/mentor/requests/m-42",
		"/healthz":                     "/healthz",
	}

	for path, want := range cases {
		if got := Path(path); got != want {
			t.Errorf("Path(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestURLRedactsPathAndQuery(t *testing.T) {
	cases := map[string]string{
		"http://worker:8090/jobs/new-request-watcher?requestId=" + sentinel: "http://worker:8090/jobs/new-request-watcher?requestId=" +
			Placeholder,
		"/api/v1/reviews/" + sentinel + "/check?status=done": "/api/v1/reviews/" + Placeholder + "/check?status=done",
		"/api/v1/mentors?page=2":                             "/api/v1/mentors?page=2",
	}

	for raw, want := range cases {
		if got := URL(raw); got != want {
			t.Errorf("URL(%q) = %q, want %q", raw, got, want)
		}
	}
}

// TestTextRedactsURLsEmbeddedInProse covers the *url.Error shape: net/http
// renders the whole target URL inside the error string, so an error text is a
// sink of its own and not just a copy of the url field.
func TestTextRedactsURLsEmbeddedInProse(t *testing.T) {
	cases := map[string]string{
		// Exactly what (&http.Client{}).Do returns for a refused connection.
		`Get "http://worker:8090/jobs/new-request-watcher?requestId=` + sentinel +
			`": dial tcp 127.0.0.1:1: connect: connection refused`: `Get "http://worker:8090/jobs/new-request-watcher?requestId=` +
			Placeholder + `": dial tcp 127.0.0.1:1: connect: connection refused`,
		// Path position, and two capability segments in a row — the case a single
		// regexp pass gets wrong, because the slash between them is both the
		// terminator of the first match and the start of the second.
		"upstream failed for /api/v1/admin/mentors/" + sentinel + "/requests/" + sentinel + "/status": "upstream failed for /api/v1/admin/mentors/" +
			Placeholder + "/requests/" + Placeholder + "/status",
		// A bare pair in prose, with no `?` in front of it.
		"rejected login_token=abc.def.ghi": "rejected login_token=" + Placeholder,
		// Trailing sentence punctuation is not part of the URL.
		"timeout on /api/v1/reviews/" + sentinel + "/check.": "timeout on /api/v1/reviews/" + Placeholder + "/check.",
		// Nothing to do: prose stays byte-identical, including text that merely
		// mentions an id under a name that is not a capability parameter.
		"connection reset by peer":       "connection reset by peer",
		"mentor " + sentinel + " is new": "mentor " + sentinel + " is new",
		"":                               "",
	}

	for raw, want := range cases {
		if got := Text(raw); got != want {
			t.Errorf("Text(%q) =\n  %q\nwant\n  %q", raw, got, want)
		}
	}
}

func TestIsUUIDRejectsNearMisses(t *testing.T) {
	if !IsUUID(sentinel) {
		t.Errorf("IsUUID(%q) = false, want true", sentinel)
	}
	for _, value := range []string{
		"", "m-42", "jane-doe",
		sentinel + "0",
		strings.Replace(sentinel, "-", "x", 1),
		strings.Replace(sentinel, "1", "g", 1),
	} {
		if IsUUID(value) {
			t.Errorf("IsUUID(%q) = true, want false", value)
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
