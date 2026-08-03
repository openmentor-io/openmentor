/**
 * safeHttpUrl returns the input only if it parses as an absolute http(s) URL,
 * otherwise null.
 *
 * SECURITY (M9): mentor-supplied calendar URLs are rendered into href/iframe
 * src. Without a scheme check a malicious profile could store a `javascript:`
 * or `data:` URL that executes inside the trusted UI. React's built-in
 * `javascript:` guard is version-dependent, so we validate explicitly.
 */
export function safeHttpUrl(value: string | null | undefined): string | null {
  if (!value) return null
  try {
    const url = new URL(value)
    if (url.protocol === 'http:' || url.protocol === 'https:') {
      return url.toString()
    }
    return null
  } catch {
    return null
  }
}

// Mirrors urlUnsafeChars in api/internal/models/validation.go, widened with
// the ASCII control bytes net/url rejects outright. Wider is the safe
// direction: a value this rejects but the API would accept only costs the user
// a field error.
// eslint-disable-next-line no-control-regex
const urlUnsafeChars = /["'<>`\\\s\u0000-\u001f\u007f]/

// Scheme plus authority, matched against the RAW string rather than a parsed
// URL. The WHATWG parser normalizes exactly where net/url does not: it reads
// host "cal.com" out of "https:/cal.com/x" and "https:///cal.com/x", while
// url.Parse finds no host in either.
const httpsAuthority = /^https:\/\/([^/?#]*)/i

/**
 * isValidCalendarUrl mirrors the API's `https_url` binding tag
 * (api/internal/models/validation.go) for the optional calendar-link field.
 * Empty passes because the field is `omitempty` on all three request structs.
 *
 * It must stay at least as strict as the tag: a value the browser accepts but
 * the API rejects comes back as a 400 with no field attached, which every
 * calendar-link form surfaces as an unrecoverable generic error. The test
 * table is the shared one from api/test/internal/models/
 * calendar_url_validation_test.go — keep both in step.
 */
export function isValidCalendarUrl(value?: string | null): boolean {
  if (!value) return true
  if (urlUnsafeChars.test(value)) return false

  const authority = httpsAuthority.exec(value)?.[1]
  // An '@' means userinfo, which the tag rejects via url.User != nil — note
  // a bare "https://@cal.com" counts, so testing url.username is not enough.
  if (!authority || authority.includes('@')) return false

  // Whatever is left is structural (port range, IPv6 brackets); the parser
  // rules on it.
  try {
    return new URL(value).host !== ''
  } catch {
    return false
  }
}
