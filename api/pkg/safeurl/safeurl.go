// Package safeurl holds the ONE definition of "a URL this system will accept",
// so the binding tag on a request struct and the startup check on a config
// value cannot drift apart. It was extracted from
// internal/models.validateHTTPSURL, which internal/models still delegates to as
// the `https_url` tag; api/config uses the same functions on BASE_URL, the
// worker's Discord invite link and the event-trigger URLs.
package safeurl

import (
	"net/url"
	"strings"
)

// unsafeChars would let a URL break out of the HTML attribute, the plaintext
// email line or the log field it is rendered into. url.Parse happily keeps
// them in the path.
const unsafeChars = "\"'<>`\\ \t\r\n"

// IsHTTPS reports whether raw is an absolute https URL with a host and no
// userinfo.
//
// validator's built-in `url` tag is not a substitute: it only requires a scheme
// plus a host or opaque part, so `javascript:alert(1)`, `data:text/html,...`,
// `mailto:a@b.c` and `https://evil.example/x">` all pass it (the
// scheme-restricting tag is the separate, unused `http_url`).
func IsHTTPS(raw string) bool {
	u, ok := parse(raw)
	return ok && u.Scheme == "https"
}

// IsHTTPOrHTTPS is IsHTTPS widened to plain http, for the URLs that are only
// ever dialed inside the Docker network and so have no TLS to speak of — the
// *_TRIGGER_URL values are all `http://worker:8090/jobs/...`. The scheme
// allowlist is still the point: it is what stops a typo'd trigger URL from
// becoming a `file:` or `javascript:` value the API then hands to an HTTP
// client.
func IsHTTPOrHTTPS(raw string) bool {
	u, ok := parse(raw)
	return ok && (u.Scheme == "https" || u.Scheme == "http")
}

func parse(raw string) (*url.URL, bool) {
	if strings.ContainsAny(raw, unsafeChars) {
		return nil, false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, false
	}
	if u.Host == "" || u.User != nil {
		return nil, false
	}
	return u, true
}
