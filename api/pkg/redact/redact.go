// Package redact holds the rules that keep capability-bearing values and
// personal data out of telemetry. A review request_id is a bearer token —
// anyone holding it can read the mentor's name and submit a review for that
// mentee/mentor pair — and magic-link/confirmation tokens are login
// credentials. An email address is not a capability but it is personal data
// under GDPR and an account-enumeration oracle for anyone with log access
// (C11). None of them may reach a log line, a span attribute or an analytics
// property (P14).
//
// This is the ONE place those rules live. Call-site helpers were tried first
// and drifted: before C11 there were three independent email maskers (one in
// internal/services, one here-shaped-but-absent, one in web/src/lib/pii.ts) and
// a hand-rolled DSN mask in cmd/migrate that was a blind byte-prefix. The Go
// half is now this package alone; the TypeScript half is web/src/lib/pii.ts,
// pinned to the same behavior by the shared fixture in testdata/.
package redact

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strings"
)

// Placeholder is written in place of a redacted value.
const Placeholder = "[REDACTED]"

// EmailMask is what an address collapses to when it has no usable local part or
// domain to keep. It is deliberately NOT Placeholder: a masked address is a
// value that was handled correctly, and mixing it with the credential
// placeholder would make the two indistinguishable in Loki.
const EmailMask = "***"

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
//
// The URL rules are not enough on their own, so two shape sweeps run after
// them: see sweepIDs and sweepReviewTokens.
func Text(raw string) string {
	// Every URL-shaped rule needs one of these; prose has none of them. The
	// review-token and email sweeps are checked separately because neither needs
	// any of them (`rvw_` plus a base64url body can be letters and digits only,
	// and `a@b.co` is letters, digits, `@` and `.`).
	if !strings.ContainsAny(raw, "/=-") {
		return sweepEmails(sweepReviewTokens(raw))
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
	// Emails last: the URL rules above run per token and can hand back a token
	// with an address still inside it (`?email=a@b.co` keeps its value, because
	// "email" is not a capability key), and sweepIDs can only ever shorten.
	return sweepEmails(sweepIDs(sweepReviewTokens(b.String())))
}

// reviewTokenPrefix and reviewTokenLen describe the shape of a review capability
// minted by pkg/reviewtoken (H4). Duplicated rather than imported so this package
// stays a dependency-free leaf; a test in pkg/reviewtoken pins the two
// definitions together.
const (
	reviewTokenPrefix = "rvw_"
	reviewTokenLen    = len(reviewTokenPrefix) + 43 // base64url of 32 bytes
)

// sweepReviewTokens removes review capability tokens from free-form text.
//
// The key-name rules cannot find these on their own: the token only ever reaches
// the API in a JSON body, so if it does surface in a log line it will be inside a
// message like `no live invitation for rvw_…` or a driver error quoting the
// parameter it bound — text with no `key=` pair and no path segment for
// QueryString or Path to bite on.
//
// Dropped outright rather than hashed like a request id: there is nothing to
// correlate a spent token against, and a hashed form in a log line would be the
// exact value review_invitations.token_hash is looked up by.
func sweepReviewTokens(text string) string {
	if !strings.Contains(text, reviewTokenPrefix) {
		return text
	}

	var b strings.Builder
	copied := 0
	for i := 0; i+reviewTokenLen <= len(text); i++ {
		if !strings.HasPrefix(text[i:], reviewTokenPrefix) {
			continue
		}
		// Anchored on the left so `xrvw_…` inside a longer opaque value is not
		// mistaken for a token start.
		if i > 0 && isTokenChar(text[i-1]) {
			continue
		}
		if !isTokenBody(text[i+len(reviewTokenPrefix) : i+reviewTokenLen]) {
			continue
		}
		b.WriteString(text[copied:i])
		b.WriteString(Placeholder)
		i += reviewTokenLen - 1
		copied = i + 1
	}
	if copied == 0 {
		return text
	}
	b.WriteString(text[copied:])
	return b.String()
}

func isTokenBody(body string) bool {
	for i := 0; i < len(body); i++ {
		if !isTokenChar(body[i]) {
			return false
		}
	}
	return true
}

// isTokenChar covers the base64url alphabet, which is also what a `_`-separated
// prefix is made of.
func isTokenChar(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
		return true
	}
	return false
}

// sweepIDs replaces every UUID left in the text with a hashed reference. The
// URL rules above only fire on tokens shaped like a URL or a name=value pair,
// and the error text that actually reaches a log line often has neither: the
// repository layer names the row it failed on (`failed to fetch client request
// <uuid>`), a handler attaches `failed to fetch request id="<uuid>"`, and
// Postgres reports the offending value itself in a constraint DETAIL
// (`Key (id)=(<uuid>) already exists`). Hashed rather than dropped, so the line
// still ties to the request_ref logged next to it.
//
// This runs last on purpose: path and query rules have already replaced the ids
// they own, so what is left here is only what they cannot see.
func sweepIDs(text string) string {
	const uuidLen = 36
	if len(text) < uuidLen {
		return text
	}

	var b strings.Builder
	copied := 0
	for i := 0; i+uuidLen <= len(text); i++ {
		// Anchor on both ends: a 36-character window inside a longer hex run is
		// part of a different value, not a UUID.
		if i > 0 && isIDChar(text[i-1]) {
			continue
		}
		if i+uuidLen < len(text) && isIDChar(text[i+uuidLen]) {
			continue
		}
		if !IsUUID(text[i : i+uuidLen]) {
			continue
		}
		b.WriteString(text[copied:i])
		b.WriteString(ID(text[i : i+uuidLen]))
		i += uuidLen - 1
		copied = i + 1
	}
	if copied == 0 {
		return text
	}
	b.WriteString(text[copied:])
	return b.String()
}

func isIDChar(c byte) bool { return isHexDigit(c) || c == '-' }

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

	// Before the URL rules, because none of them look at userinfo: a driver that
	// fails to connect renders the whole DSN it was handed, password included.
	trimmed = redactURLUserinfo(trimmed)

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

// Email masks an address for telemetry: the first character of the local part
// and the WHOLE domain survive, the rest of the local part does not.
//
// # WHY THIS SHAPE, AND WHY NOT ID
//
// The domain is kept because it is what an operator actually debugs with — SES
// bounces, suppression-list entries and greylisting are domain-wide, so
// `[…]@corp.example` tells you the shape of the failure while carrying no
// address anyone can mail. The first local character keeps a masked address
// stable per person, which is what makes "this recipient failed at 12:01 and
// succeeded at 12:05" answerable from the logs alone.
//
// ID is deliberately NOT used on addresses. It is right for a request id — 122
// bits of entropy make the hash a pure correlation handle — but an email address
// has almost none against a guessed candidate list, so sha256(address) in Loki
// is a "confirm this person is a user" oracle for every log reader, which is the
// enumeration risk C11 exists to remove. Masking loses uniqueness; hashing loses
// the whole point.
func Email(address string) string {
	trimmed := strings.TrimSpace(address)
	if trimmed == "" {
		return ""
	}

	// LastIndex, not Index: a local part may legally contain a quoted `@`, and
	// the domain never does.
	at := strings.LastIndex(trimmed, "@")
	switch {
	case at <= 0, at == len(trimmed)-1:
		// No local part or no domain. Not an address, so nothing here is safe to
		// keep — the input could be anything, including a token.
		return EmailMask
	}

	local, domain := trimmed[:at], trimmed[at:]
	if len(local) == 1 {
		return "*" + domain
	}
	return local[:1] + EmailMask + domain
}

// sweepEmails masks every address embedded in free-form text. This is the sweep
// that makes the whole C11 fix hold: a call site can be fixed by hand, but the
// address also arrives through errors nobody writes by hand — SES quotes the
// destination it rejected, and Postgres quotes the offending value in a
// unique-violation DETAIL (`Key (email)=(…) already exists`). Running here means
// logger.RedactedError and errortracking's redact.Text call both inherit it.
func sweepEmails(text string) string {
	if !strings.Contains(text, "@") {
		return text
	}

	var b strings.Builder
	copied := 0
	for i := 0; i < len(text); i++ {
		if text[i] != '@' {
			continue
		}
		start := i
		for start > 0 && isEmailLocalChar(text[start-1]) {
			start--
		}
		end := i + 1
		for end < len(text) && isEmailDomainChar(text[end]) {
			end++
		}
		// A trailing dot or dash belongs to the sentence, not the domain:
		// "wrote to a@b.co." and "a@b.co-").
		for end > i+1 && (text[end-1] == '.' || text[end-1] == '-') {
			end--
		}
		if start == i || !isEmailDomain(text[i+1:end]) {
			continue
		}
		// Do not re-cut an address already masked upstream: Email is idempotent,
		// but only because `*` is not a local-part character, so `j***@b.co`
		// never reaches here with a local part to strip.
		b.WriteString(text[copied:start])
		b.WriteString(Email(text[start:end]))
		i = end - 1
		copied = end
	}
	if copied == 0 {
		return text
	}
	b.WriteString(text[copied:])
	return b.String()
}

// isEmailDomain requires a dot and an alphabetic TLD. Both are load-bearing
// against false positives that matter: a Go module path (`example.com/pkg@v1.2.3`
// — TLD `3`), a docker image tag (`alloy@sha256:…` — no dot after the `@`) and a
// libpq host with no domain (`user:pw@postgres` — handled by redactURLUserinfo
// instead).
func isEmailDomain(domain string) bool {
	dot := strings.LastIndexByte(domain, '.')
	if dot <= 0 || dot == len(domain)-1 {
		return false
	}
	tld := domain[dot+1:]
	if len(tld) < 2 {
		return false
	}
	for i := 0; i < len(tld); i++ {
		c := tld[i]
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') {
			return false
		}
	}
	return true
}

func isEmailLocalChar(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	case c == '.', c == '_', c == '%', c == '+', c == '-':
		return true
	}
	return false
}

func isEmailDomainChar(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	case c == '.', c == '-':
		return true
	}
	return false
}

// DSN returns a loggable form of a database connection string. The scheme, user,
// host, port and database name survive — the user in particular is worth keeping,
// because since H8 it names WHICH role the process connected as (om_migrate vs
// om_backend), which is the first thing to check when a migration is refused.
// The password does not survive.
//
// It parses with net/url rather than slicing bytes. The mask it replaces was
// `url[:20] + "***"`, which is not a mask at all: it keeps a fixed 20-byte
// prefix, and `len("postgres://")` is 11, so it leaks byte 9 onward of whatever
// follows the username. With `openmentor` (every template in the repo) that is
// zero password bytes; with a username shorter than 8 characters it is password
// bytes, silently, with no test and no error (C11).
func DSN(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		// Not a URL DSN. The other shape libpq accepts is keyword/value
		// (`host=… user=… password=…`), which Text already handles token by
		// token via the sensitive-key rules — and anything genuinely malformed is
		// safer through those rules than echoed.
		return Text(stripDSNUserinfo(trimmed))
	}

	// Built by hand instead of parsed.String(): url.UserPassword percent-encodes
	// the mask (`***` becomes `%2A%2A%2A`), which is unreadable in a log line.
	var b strings.Builder
	b.WriteString(parsed.Scheme)
	b.WriteString("://")
	if parsed.User != nil {
		b.WriteString(parsed.User.Username())
		if _, hasPassword := parsed.User.Password(); hasPassword {
			b.WriteString(":" + EmailMask)
		}
		b.WriteString("@")
	}
	b.WriteString(parsed.Host)
	b.WriteString(parsed.Path)
	if query := QueryString(parsed.RawQuery); query != "" {
		b.WriteString("?")
		b.WriteString(query)
	}
	return b.String()
}

// redactURLUserinfo removes the password from the userinfo of a URL embedded in
// text, returning the REDACTED url — the name says userinfo rather than password
// for that reason, and because CodeQL's sensitive-data heuristic reads function
// names: called stripURLPassword, its return value was labeled a password, and
// the label then followed Text into redact.ID's SHA-256 as a bogus
// "password hashed with a fast hash" finding.
//
// Kept separate from DSN because it must not reject what it cannot parse:
// it works on one token lifted out of an error message, where the URL may be
// truncated or quoted, and it has to leave everything it does not understand
// exactly as it found it.
func redactURLUserinfo(token string) string {
	scheme := strings.Index(token, "://")
	if scheme < 0 {
		return token
	}

	start := scheme + len("://")
	// Userinfo ends at the LAST `@` before the path, so a password containing
	// `@` is still fully removed.
	end := len(token)
	if slash := strings.IndexByte(token[start:], '/'); slash >= 0 {
		end = start + slash
	}
	at := strings.LastIndexByte(token[start:end], '@')
	if at < 0 {
		return token
	}
	userinfo := token[start : start+at]
	colon := strings.IndexByte(userinfo, ':')
	if colon < 0 {
		return token
	}
	return token[:start+colon+1] + EmailMask + token[start+at:]
}

// stripDSNUserinfo masks a `user:password@` segment without requiring the `://`
// that redactURLUserinfo anchors on. Only DSN's fallback uses it: a connection
// string too malformed for net/url still carries a real password, and that is
// exactly the input the fallback exists for. It is not used on free-form text,
// where a bare `note:see me@host` would be a false positive.
func stripDSNUserinfo(raw string) string {
	at := strings.LastIndexByte(raw, '@')
	if at < 0 {
		return raw
	}
	head := raw[:at]
	colon := strings.LastIndexByte(head, ':')
	// A colon that opens `://` is the scheme separator, not a password delimiter.
	if colon < 0 || strings.HasPrefix(head[colon:], "://") {
		return raw
	}
	return head[:colon+1] + EmailMask + raw[at:]
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
