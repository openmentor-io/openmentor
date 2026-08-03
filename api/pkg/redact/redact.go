// Package redact holds the rules that keep capability-bearing values out of
// telemetry. A review request_id is a bearer token — anyone holding it can read
// the mentor's name and submit a review for that mentee/mentor pair — and
// magic-link/confirmation tokens are login credentials. None of them may reach
// a log line, a span attribute or an analytics property (P14).
package redact

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strings"
)

// Placeholder is written in place of a redacted value.
const Placeholder = "[REDACTED]"

// sensitiveKeyParts match ANYWHERE in a normalized key. Substring matching is
// deliberate: an exact-key list let login_token, confirm_token, captchaToken
// and request_id through.
var sensitiveKeyParts = []string{
	"token",
	"secret",
	"password",
	"credential",
	"apikey",
	"auth",
	"session",
	"cookie",
	"signature",
	"captcha",
	"requestid",
	"otp",
}

// sensitiveWholeKeys match the WHOLE normalized key. "key" cannot be a
// substring rule without also matching keyword and monkey, but a bare ?key=
// parameter is a credential.
var sensitiveWholeKeys = map[string]struct{}{
	"key": {},
}

// SensitiveKey reports whether a query parameter, path parameter or log field
// name may carry a credential or a capability.
func SensitiveKey(key string) bool {
	normalized := NormalizeKey(key)
	if normalized == "" {
		return false
	}
	if _, found := sensitiveWholeKeys[normalized]; found {
		return true
	}
	for _, part := range sensitiveKeyParts {
		if strings.Contains(normalized, part) {
			return true
		}
	}
	return false
}

// NormalizeKey folds case and separators so request_id, requestId, REQUEST-ID
// and request%5Fid all compare equal.
func NormalizeKey(key string) string {
	var b strings.Builder
	b.Grow(len(key))
	for _, r := range strings.ToLower(strings.TrimSpace(key)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Query returns a loggable copy of values: sensitive parameters keep their name
// (knowing the parameter was present is useful) but lose their value.
func Query(values url.Values) map[string]string {
	redacted := make(map[string]string, len(values))
	for key, list := range values {
		switch {
		case SensitiveKey(key):
			redacted[key] = Placeholder
		case len(list) > 0:
			redacted[key] = list[0]
		}
	}
	return redacted
}

// QueryString redacts the values of sensitive parameters in a raw query string,
// leaving the rest of it (and its encoding) untouched.
func QueryString(raw string) string {
	if raw == "" {
		return raw
	}

	pairs := strings.Split(raw, "&")
	for i, pair := range pairs {
		name := pair
		if idx := strings.Index(pair, "="); idx >= 0 {
			name = pair[:idx]
		}
		// A percent-encoded name still has to match: request%5Fid is request_id.
		decoded, err := url.QueryUnescape(name)
		if err != nil {
			decoded = name
		}
		if SensitiveKey(decoded) {
			pairs[i] = name + "=" + Placeholder
		}
	}
	return strings.Join(pairs, "&")
}

// IsUUID reports whether a value is shaped like one of our primary keys, which
// is the only rule that catches a capability arriving under an innocent name:
// the :id in /api/v1/mentor/requests/:id is the SAME client_requests.id that
// /api/v1/reviews/:requestId accepts as authorization, so the parameter's name
// says nothing about whether its value grants access.
func IsUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
			continue
		}
		if !isHexDigit(c) {
			return false
		}
	}
	return true
}

func isHexDigit(c byte) bool {
	switch {
	case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F':
		return true
	}
	return false
}

// Path redacts capability-bearing segments of a URL path. It works on the raw
// path rather than on the matched route params, so it also covers unmatched
// routes (a 404 has no params to inspect).
func Path(path string) string {
	// Every id we redact is a UUID, and a UUID always has dashes — this keeps the
	// common path off the allocating branch.
	if !strings.Contains(path, "-") {
		return path
	}

	segments := strings.Split(path, "/")
	redacted := false
	for i, segment := range segments {
		if IsUUID(segment) {
			segments[i] = Placeholder
			redacted = true
		}
	}
	if !redacted {
		return path
	}
	return strings.Join(segments, "/")
}

// URL redacts a URL or a request target: sensitive query values are dropped and
// capability-bearing path segments are stripped.
func URL(raw string) string {
	if idx := strings.Index(raw, "?"); idx >= 0 {
		return Path(raw[:idx]) + "?" + QueryString(raw[idx+1:])
	}
	return Path(raw)
}

// Text redacts capability-bearing values inside free-form text: an error
// message, a response body preview, a span name. URL is not usable here because
// the string is not a URL — it CONTAINS one, in the middle of a sentence.
//
// The case that matters is *url.Error, which is what http.Client.Do returns:
// its Error() renders `Get "<the whole target URL>": dial tcp ...`, so
// zap.Error(err) re-leaks the id that the explicit url field dropped (P14).
//
// Tokenizing on characters that cannot appear inside a URL and running the
// existing URL/Path/QueryString rules per token is what keeps this correct where
// a regex over the whole string is not: consecutive capability segments
// (/mentors/<uuid>/requests/<uuid>) need the slash between them to be both a
// terminator and a separator, which Path's split already handles and a
// non-backtracking regexp does not.
func Text(raw string) string {
	// Every rule needs one of these; prose has neither.
	if !strings.ContainsAny(raw, "/=") {
		return raw
	}

	var b strings.Builder
	b.Grow(len(raw))
	start := 0
	for i := 0; i <= len(raw); i++ {
		if i < len(raw) && !isTextBoundary(raw[i]) {
			continue
		}
		if i > start {
			b.WriteString(redactTextToken(raw[start:i]))
		}
		if i < len(raw) {
			b.WriteByte(raw[i])
		}
		start = i + 1
	}
	return b.String()
}

// isTextBoundary reports whether c ends a URL-shaped token. url.Error wraps the
// target in double quotes and follows it with `: `, and log messages put URLs in
// parentheses and lists.
func isTextBoundary(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\r', '"', '\'', '`', '<', '>', ',', ';', '(', ')', '[', ']', '{', '}':
		return true
	}
	return false
}

func redactTextToken(token string) string {
	// Sentence punctuation is not part of the URL: `... /reviews/<uuid>/check.`
	trimmed := strings.TrimRight(token, ".:;!?")
	suffix := token[len(trimmed):]

	switch {
	case strings.Contains(trimmed, "?"):
		return URL(trimmed) + suffix
	// A bare pair with no `?` in front of it: `rejected login_token=<jwt>`.
	case strings.Contains(trimmed, "="):
		return QueryString(trimmed) + suffix
	case strings.Contains(trimmed, "/"):
		return Path(trimmed) + suffix
	}
	return token
}

// ID returns a short, non-reversible reference for a capability value so log
// lines about the same request can still be correlated without carrying the
// value that grants access.
func ID(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(trimmed))
	return "sha256:" + hex.EncodeToString(sum[:])[:12]
}
