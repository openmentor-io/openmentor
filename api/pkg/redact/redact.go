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

// SensitiveKey reports whether a query parameter, path parameter or log field
// name may carry a credential or a capability.
func SensitiveKey(key string) bool {
	normalized := NormalizeKey(key)
	if normalized == "" {
		return false
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
