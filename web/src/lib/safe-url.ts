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
