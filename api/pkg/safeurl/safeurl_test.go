package safeurl_test

import (
	"testing"

	"github.com/openmentor-io/openmentor/api/pkg/safeurl"
	"github.com/stretchr/testify/assert"
)

// The table is shared with the calendar-URL binding cases in
// test/internal/models/calendar_url_validation_test.go and the client-side mirror
// in web/src/lib/__tests__/safe-url.test.ts. All three must agree: a value the
// browser accepts but the API rejects surfaces as a 400 with no field attached.
func TestIsHTTPS(t *testing.T) {
	tests := []struct {
		raw  string
		want bool
	}{
		{"https://calendly.com/jane", true},
		{"https://cal.com/jane/30min?month=2026-08", true},
		{"https://cal.example:8443/jane", true},

		{"", false},
		// The whole reason validator's `url` tag is not a substitute.
		{"javascript:alert(1)", false},
		{"data:text/html,<script>alert(1)</script>", false},
		{"mailto:jane@example.com", false},
		{"file:///etc/passwd", false},
		{"http://calendly.com/jane", false},
		// Scheme-relative and relative: no host as far as net/url is concerned.
		{"//calendly.com/jane", false},
		{"/jane", false},
		{"calendly.com/jane", false},
		// Would break out of the href attribute it is rendered into.
		{`https://evil.example/x">`, false},
		{"https://evil.example/x'", false},
		{"https://evil.example/ x", false},
		{"https://evil.example/x\n", false},
		// Userinfo: the credential-in-URL phishing shape.
		{"https://user:pass@calendly.com/jane", false},
		{"https://@calendly.com/jane", false},
	}

	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			assert.Equal(t, tt.want, safeurl.IsHTTPS(tt.raw))
		})
	}
}

// TestIsHTTPOrHTTPS: the trigger URLs are dialed inside the Docker network, so
// plain http is correct there — but the scheme allowlist still has to hold.
func TestIsHTTPOrHTTPS(t *testing.T) {
	tests := []struct {
		raw  string
		want bool
	}{
		{"http://worker:8090/jobs/new-mentor-watcher?mentorId=", true},
		{"https://worker:8090/jobs/x", true},
		{"", false},
		{"/jobs/x", false},
		{"worker:8090/jobs/x", false},
		{"javascript:alert(1)", false},
		{"file:///etc/passwd", false},
		{"http://user:pass@worker:8090/jobs/x", false},
	}

	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			assert.Equal(t, tt.want, safeurl.IsHTTPOrHTTPS(tt.raw))
		})
	}
}
