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

// Mirrors urlUnsafeChars in api/internal/models/validation.go. \s is wider
// than the API's set, which is the safe direction: a value this rejects but
// the API would accept only costs the user a field error.
const urlUnsafeChars = /["'<>`\\\s]/

/**
 * isValidCalendarUrl mirrors the API's `https_url` binding tag
 * (api/internal/models/validation.go) for the optional calendar-link field.
 * Empty passes because the field is `omitempty` on all three request structs.
 *
 * It must stay at least as strict as the tag: a value the browser accepts but
 * the API rejects comes back as a 400 with no field attached, which every
 * calendar-link form surfaces as an unrecoverable generic error. `new URL()`
 * alone is not strict enough — it tolerates surrounding whitespace and
 * userinfo, both of which the tag rejects.
 */
export function isValidCalendarUrl(value?: string | null): boolean {
  if (!value) return true
  if (urlUnsafeChars.test(value)) return false
  try {
    const url = new URL(value)
    return url.protocol === 'https:' && url.host !== '' && !url.username && !url.password
  } catch {
    return false
  }
}
