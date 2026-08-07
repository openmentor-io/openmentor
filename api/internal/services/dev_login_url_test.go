package services

import (
	"strings"
	"testing"

	"github.com/openmentor-io/openmentor/api/pkg/redact"
)

// TestDevLoginURLBypassesTheLogger documents WHY the development magic link is
// written to stdout instead of logged, and pins both halves of the reason (C11).
//
// A magic-link URL carries a live login token. Since C11 the logger's redacting
// core rewrites `?token=…` in every field it encodes — correct for a log line,
// and fatal for this branch, whose entire purpose is to hand a developer a
// clickable link when no email trigger is configured. Writing to stdout keeps the
// credential out of app.log and error.log as a bonus.
func TestDevLoginURLBypassesTheLogger(t *testing.T) {
	const loginURL = "http://localhost:3000/mentor/auth/callback?token=devtoken123"

	var out strings.Builder
	previous := devLoginOut
	devLoginOut = &out
	t.Cleanup(func() { devLoginOut = previous })

	printDevLoginURL("DEVELOPMENT MENTOR LOGIN URL", loginURL)

	if !strings.Contains(out.String(), loginURL) {
		t.Errorf("the link was not written verbatim, so local login is broken: %q", out.String())
	}

	// The other half: had it stayed a log field, the token would be gone. This is
	// the assertion that stops someone "tidying" it back into logger.Info.
	if redacted := redact.Text(loginURL); strings.Contains(redacted, "devtoken123") {
		t.Errorf("redact.Text left the login token in place: %q", redacted)
	} else if !strings.Contains(redacted, redact.Placeholder) {
		t.Errorf("redact.Text(%q) = %q, expected the token replaced with %q", loginURL, redacted, redact.Placeholder)
	}
}
