/**
 * Rules that keep capability-bearing values out of telemetry.
 *
 * SECURITY (P14): a review `request_id` is a bearer token — whoever holds it can
 * read the mentor's name and submit a review as that mentee — and the magic-link
 * `token` is a login credential. Both travel in URLs, so they reach logs, span
 * attributes and analytics unless they are stripped on the way in.
 */

export const REDACTED = '[REDACTED]'

/**
 * Substrings that make a key sensitive, matched against its separator-free
 * spelling. Substring matching is deliberate: an exact-key list is what let
 * `login_token`, `confirm_token` and `request_id` through.
 */
const SENSITIVE_KEY_PARTS = [
  'token',
  'secret',
  'password',
  'credential',
  'apikey',
  'auth',
  'session',
  'cookie',
  'signature',
  'captcha',
  'requestid',
  'otp',
] as const

/** Log/span fields whose value is a URL and needs the query-string treatment. */
const URL_VALUED_KEYS = new Set([
  'url',
  'urlfull',
  'urlquery',
  'urlpath',
  'httpurl',
  'httptarget',
  'route',
  'href',
  'location',
  'referer',
  'referrer',
  'requesturl',
  'loginurl',
  'confirmurl',
])

/** UUID occupying a whole path segment, at any depth. */
const UUID_SEGMENT = /\/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}(?=\/|$)/gi

/**
 * `key=value` pairs whose key looks sensitive, in any casing and with the
 * separator written literally or percent-encoded. The leading delimiter is `?`,
 * `&` or the start of the string — a bare `url.query` span attribute has no `?`.
 * This runs over free text as well, which is what catches a token embedded in an
 * error message or a stack trace rather than sitting in its own field.
 */
const SENSITIVE_QUERY_PAIR = new RegExp(
  `(^|[?&])([^=&\\s]*(?:${SENSITIVE_KEY_PARTS.map((part) =>
    part === 'requestid' ? 'request(?:_|-|%5f)?id' : part
  ).join('|')})[^=&\\s]*=)([^&\\s"'\\\\]*)`,
  'gi'
)

export function normalizeKey(key: string): string {
  return key.toLowerCase().replace(/[^a-z0-9]/g, '')
}

export function isSensitiveKey(key: string): boolean {
  const normalized = normalizeKey(key)
  if (!normalized) return false
  return SENSITIVE_KEY_PARTS.some((part) => normalized.includes(part))
}

/** Redacts the values of sensitive query parameters anywhere in a string. */
export function redactQueryValues(value: string): string {
  return value.replace(SENSITIVE_QUERY_PAIR, `$1$2${REDACTED}`)
}

/**
 * Turns a URL into something loggable: id-bearing path segments become `:id`,
 * sensitive query values are replaced. The origin (if any) is kept.
 */
export function redactUrl(url: string | undefined): string {
  if (!url) return ''

  const queryStart = url.indexOf('?')
  const path = queryStart === -1 ? url : url.slice(0, queryStart)
  const query = queryStart === -1 ? '' : url.slice(queryStart)

  return path.replace(UUID_SEGMENT, '/:id') + redactQueryValues(query)
}

/**
 * Truncates a client IP to its network prefix. Logs go to Loki, where a full
 * address is personal data under GDPR and, combined with a timestamp, identifies
 * an individual visitor.
 */
export function maskIp(ip: string | string[] | undefined): string {
  const first = Array.isArray(ip) ? ip[0] : ip
  if (!first) return ''
  // X-Forwarded-For is a list; the left-most entry is the client.
  const client = first.split(',')[0].trim()
  if (!client) return ''

  if (client.includes('.')) {
    const octets = client.split('.')
    return octets.length === 4 ? `${octets[0]}.${octets[1]}.${octets[2]}.0` : REDACTED
  }
  if (client.includes(':')) {
    // Loopback and other addresses with no network prefix have nothing to drop.
    if (client.startsWith('::')) return client
    // Otherwise keep the routing prefix and drop the interface identifier.
    return `${client.split(':').slice(0, 3).filter(Boolean).join(':')}::`
  }
  return REDACTED
}

/**
 * Deep-scrubs a log record or span attribute bag: sensitive keys lose their
 * value, URL-valued keys are normalized, and every other string is still swept
 * for embedded `?token=` pairs.
 */
export function redactValue(value: unknown, key = ''): unknown {
  if (typeof value === 'string') {
    if (key && isSensitiveKey(key)) return REDACTED
    if (key && URL_VALUED_KEYS.has(normalizeKey(key))) return redactUrl(value)
    return redactQueryValues(value)
  }

  if (Array.isArray(value)) {
    return value.map((item) => redactValue(item, key))
  }

  if (value instanceof Error) {
    return value
  }

  if (value && typeof value === 'object') {
    const redacted: Record<string, unknown> = {}
    for (const [childKey, childValue] of Object.entries(value as Record<string, unknown>)) {
      redacted[childKey] = isSensitiveKey(childKey)
        ? REDACTED
        : redactValue(childValue, childKey)
    }
    return redacted
  }

  return value
}
