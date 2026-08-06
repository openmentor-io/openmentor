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

/**
 * Keys that are sensitive as a WHOLE key only: `key` cannot be a substring rule
 * without also matching keyword, monkey and keyboard, but a bare `?key=` is a
 * credential.
 */
const SENSITIVE_WHOLE_KEYS = ['key'] as const

/**
 * PostHog's own reserved properties, which the substring rules above would
 * otherwise destroy: `$session_id` normalizes to `sessionid` and so matched the
 * `session` rule, turning EVERY session id into the same `[REDACTED]` string.
 * That collapses session grouping, session-replay correlation and the
 * exception-to-session link (`instrumentation.ts` sends `$session_id` on server
 * exceptions) on every event, `$snapshot` included. None of these authorizes
 * anything — the SDK mints them in the browser.
 *
 * Matched on the RAW key with its `$` intact, which is what makes the exemption
 * safe: `$` is PostHog's reserved namespace, so `$session_id` survives while an
 * application cookie named `sessionid` — identical once normalized — does not.
 *
 * Verified against posthog-js 1.409.5 by matching the rules against every
 * `$`-prefixed reserved property name in its bundle.
 */
const RESERVED_TELEMETRY_KEYS = new Set([
  '$session_id',
  // Not currently matched, but it is the id `$session_id` is always paired with
  // and it must not start being eaten if the rules grow.
  '$window_id',
  '$session_is_sampled',
  // PostHog's own flag-evaluation request id, unrelated to our `request_id`.
  '$feature_flag_request_id',
])

/**
 * Open-ended reserved families, matched by prefix: `$session_entry_url`,
 * `$session_entry_pathname`, `$session_entry_utm_*`,
 * `$session_recording_start_reason`. These are exempt from the KEY rules only —
 * every one of them is still swept as a free-form string, so
 * `$session_entry_url` keeps its shape and loses the capability path.
 */
const RESERVED_TELEMETRY_PREFIXES = ['$session_entry_', '$session_recording_'] as const

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

/**
 * UUID occupying a whole path segment, at any depth. The trailing boundary is a
 * negative lookahead rather than `(?=\/|$)` so it also fires mid-string, where
 * the segment ends at `?`, `#` or a quote — `$current_url` and
 * `$elements_chain` both carry the path with something after it.
 */
const UUID_SEGMENT = /\/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}(?![0-9a-z_-])/gi

/**
 * Parts that need a pattern rather than the literal substring, keyed by the
 * SENSITIVE_KEY_PARTS entry they replace.
 */
const QUERY_PAIR_PATTERNS: Record<string, string> = {
  // request_id, requestId, request-id and the percent-encoded request%5Fid.
  requestid: 'request(?:_|-|%5f)?id',
  // `otp` is only three characters, so as a bare substring it also fires inside
  // words (botpageview, notpurchased) and, worse, inside a base64 blob that ends
  // in `=` padding — which would rewrite a replay's inline image. The lookahead
  // keeps `otp`, `otp_code` and `loginOtp` and drops the accidents.
  otp: 'otp(?![a-z])',
}

/**
 * `key=value` pairs whose key looks sensitive, in any casing and with the
 * separator written literally or percent-encoded. The leading delimiter is `?`,
 * `&`, `#`, whitespace or the start of the string: `?`/`&` for a URL query, `#`
 * for a URL FRAGMENT — H4 delivers the review capability as
 * `/reviews/new#review_token=…`, and in `…?a=b#review_token=…` there is no
 * `?`/`&` left for the regex to anchor on. Start-of-string because a bare
 * `url.query` span attribute has no `?`, and whitespace so a token embedded in
 * free text (`Error: rejected login_token=<jwt>`) is caught too — this runs over
 * every winston `message`.
 */
const SENSITIVE_QUERY_PAIR = new RegExp(
  `(^|[?&#\\s])((?:[^=&\\s]*(?:${SENSITIVE_KEY_PARTS.map(
    (part) => QUERY_PAIR_PATTERNS[part] ?? part
  ).join('|')})[^=&\\s]*|${SENSITIVE_WHOLE_KEYS.join('|')})=)([^&\\s"'\\\\]*)`,
  'gi'
)

export function normalizeKey(key: string): string {
  return key.toLowerCase().replace(/[^a-z0-9]/g, '')
}

/**
 * True for a PostHog-reserved telemetry identifier that must survive redaction
 * verbatim (see RESERVED_TELEMETRY_KEYS).
 */
export function isReservedTelemetryKey(key: string): boolean {
  const raw = key.trim()
  if (!raw.startsWith('$')) return false
  return (
    RESERVED_TELEMETRY_KEYS.has(raw) ||
    RESERVED_TELEMETRY_PREFIXES.some((prefix) => raw.startsWith(prefix))
  )
}

export function isSensitiveKey(key: string): boolean {
  if (isReservedTelemetryKey(key)) return false
  const normalized = normalizeKey(key)
  if (!normalized) return false
  if (SENSITIVE_WHOLE_KEYS.some((whole) => normalized === whole)) return true
  return SENSITIVE_KEY_PARTS.some((part) => normalized.includes(part))
}

/** Redacts the values of sensitive query parameters anywhere in a string. */
export function redactQueryValues(value: string): string {
  return value.replace(SENSITIVE_QUERY_PAIR, `$1$2${REDACTED}`)
}

/**
 * Replaces UUIDs that occupy a whole path segment with `:id`. The segment
 * anchors are what make this safe to run over arbitrary strings: a bare UUID
 * value (a `mentor_id` property) has no leading slash and is left alone.
 */
export function redactPathIds(value: string): string {
  return value.replace(UUID_SEGMENT, '/:id')
}

/**
 * A review capability minted by `api/pkg/reviewtoken`: the `rvw_` prefix plus 43
 * base64url characters. Duplicated here rather than shared, because the Go and
 * TypeScript sides cannot import each other; `api/pkg/reviewtoken` has a test
 * pinning its own constants against `api/pkg/redact`'s copy, and
 * `telemetry-redaction.test.ts` pins this one against the same shape.
 *
 * Needed as a SHAPE rule because the token can appear with no `key=` in front of
 * it and no `/` around it: session replay serializes DOM attribute values
 * verbatim, so a token in an `input[value]` or a `data-` attribute reaches
 * `$snapshot_data` as a bare string under an innocent key. The page deliberately
 * keeps the token out of the DOM; this is the backstop for when someone forgets.
 */
const REVIEW_TOKEN = /(^|[^A-Za-z0-9_-])rvw_[A-Za-z0-9_-]{43}(?![A-Za-z0-9_-])/g

/** Removes review capability tokens wherever they appear in a string. */
export function redactReviewTokens(value: string): string {
  if (!value.includes('rvw_')) return value
  return value.replace(REVIEW_TOKEN, `$1${REDACTED}`)
}

/**
 * Every rule that is safe over a string of unknown shape. Free-form strings need
 * the PATH rule too, not just the query rule: `GoApiClient.request` builds
 * `Go API request timeout after 10000ms: /api/v1/reviews/<uuid>/check`, and that
 * message plus its stack go to Loki through `logError` with no `key=` anywhere
 * for the query rule to bite on. All three rules are anchored (a
 * `?`/`&`/`#`/whitespace delimiter, a leading `/`, a non-token character), so a
 * bare id under an innocent key stays readable.
 */
export function redactFreeText(value: string): string {
  return redactReviewTokens(redactPathIds(redactQueryValues(value)))
}

/**
 * Turns a URL into something loggable: id-bearing path segments become `:id`,
 * sensitive query values are replaced. The origin (if any) is kept.
 *
 * The fragment is split off before the path rules run. Without that, a URL with
 * no query string at all — `/reviews/new#review_token=…`, the H4 capability link
 * — would be handed to `redactPathIds` as one long "path", where the query-pair
 * rule never runs and the token survives intact.
 */
export function redactUrl(url: string | undefined): string {
  if (!url) return ''

  const hashStart = url.indexOf('#')
  const base = hashStart === -1 ? url : url.slice(0, hashStart)
  const fragment = hashStart === -1 ? '' : url.slice(hashStart)

  const queryStart = base.indexOf('?')
  const path = queryStart === -1 ? base : base.slice(0, queryStart)
  const query = queryStart === -1 ? '' : base.slice(queryStart)

  return redactReviewTokens(
    redactPathIds(path) + redactQueryValues(query) + redactQueryValues(fragment)
  )
}

/**
 * Truncates a client IP to its network prefix. Logs go to Loki, where a full
 * address is personal data under GDPR and, combined with a timestamp, identifies
 * an individual visitor.
 *
 * Anything that does not parse as an address is replaced outright: an
 * unrecognized shape is the case where a whole address would otherwise slip
 * through, which is how `::ffff:cb00:712a` (a hex-notated IPv4) used to be
 * logged in full.
 */
export function maskIp(ip: string | string[] | undefined): string {
  const first = Array.isArray(ip) ? ip[0] : ip
  if (!first) return ''
  // X-Forwarded-For is a list; the left-most entry is the client.
  let client = first.split(',')[0].trim()
  // A zone id (fe80::1%eth0) is local routing information, not part of the
  // address, and brackets are quoting some proxies add.
  client = client.split('%')[0].replace(/^\[/, '').replace(/\]$/, '')
  if (!client) return ''

  if (!client.includes(':')) return maskIpv4(client)
  return maskIpv6(client.toLowerCase())
}

const IPV4 = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/

function parseIpv4(address: string): number[] | null {
  const match = IPV4.exec(address)
  if (!match) return null
  const octets = match.slice(1).map(Number)
  return octets.every((octet) => octet <= 255) ? octets : null
}

/** Keeps the /24 network and drops the host octet. */
function maskIpv4(address: string): string {
  const octets = parseIpv4(address)
  return octets ? `${octets[0]}.${octets[1]}.${octets[2]}.0` : REDACTED
}

function maskIpv6(address: string): string {
  const groups = parseIpv6(address)
  if (!groups) return REDACTED

  // `::` and `::1` are the unspecified and loopback addresses: no network prefix
  // to drop, and neither identifies a visitor.
  if (groups.every((group) => group === 0)) return '::'
  if (groups.slice(0, 7).every((group) => group === 0) && groups[7] === 1) return '::1'

  // ::ffff:0:0/96 (IPv4-mapped, what a dual-stack socket reports for an IPv4
  // client) and ::/96 (the deprecated IPv4-compatible form) carry a full IPv4
  // address in the low 32 bits. Masking the IPv6 prefix would leave all four
  // octets visible, so the embedded address is masked as IPv4 instead.
  const zeroPrefix = groups.slice(0, 5).every((group) => group === 0)
  if (zeroPrefix && groups[5] === 0xffff) return `::ffff:${maskedLowIpv4(groups)}`
  if (zeroPrefix && groups[5] === 0) return `::${maskedLowIpv4(groups)}`

  // Everything else keeps its /48 routing prefix and loses the rest, which
  // includes the interface identifier that pins the address to one device.
  const prefix = groups.slice(0, 3)
  while (prefix.length > 0 && prefix[prefix.length - 1] === 0) prefix.pop()
  return `${prefix.map((group) => group.toString(16)).join(':')}::`
}

/** The IPv4 address in the low 32 bits, masked to its /24. */
function maskedLowIpv4(groups: number[]): string {
  return `${groups[6] >> 8}.${groups[6] & 0xff}.${groups[7] >> 8}.0`
}

/** Expands an IPv6 literal to its eight groups, or null when it is not one. */
function parseIpv6(address: string): number[] | null {
  const halves = address.split('::')
  if (halves.length > 2) return null

  // A trailing dotted quad (::ffff:192.0.2.1, 64:ff9b::192.0.2.1) is two groups.
  const tail = halves[halves.length - 1]
  if (tail.includes('.')) {
    const lastColon = tail.lastIndexOf(':') + 1
    const octets = parseIpv4(tail.slice(lastColon))
    if (!octets) return null
    const asGroups = [(octets[0] << 8) | octets[1], (octets[2] << 8) | octets[3]]
    halves[halves.length - 1] =
      tail.slice(0, lastColon) + asGroups.map((group) => group.toString(16)).join(':')
  }

  const head = halves[0] ? halves[0].split(':') : []
  const rest = halves.length === 2 && halves[1] ? halves[1].split(':') : []
  // `::` stands for at least one omitted group; without it all eight are spelled.
  const omitted = 8 - head.length - rest.length
  if (halves.length === 1 ? omitted !== 0 : omitted < 1) return null

  const groups = [...head, ...Array<string>(omitted).fill('0'), ...rest]
  const parsed = groups.map((group) => (/^[0-9a-f]{1,4}$/.test(group) ? parseInt(group, 16) : NaN))
  return parsed.some(Number.isNaN) ? null : parsed
}

/**
 * Deep-scrubs a log record or span attribute bag: sensitive keys lose their
 * value, URL-valued keys are normalized, and every other string is still swept
 * for embedded `?token=` pairs.
 */
export function redactValue(value: unknown, key = ''): unknown {
  // A capability or credential is always a string, so a shape-matched key
  // carrying a number or a boolean is a derived fact worth keeping
  // (has_request_id, sessions_count) — the same exemption the analytics
  // blocklists make.
  if (typeof value === 'number' || typeof value === 'boolean') return value

  if (key && isSensitiveKey(key)) return REDACTED

  if (typeof value === 'string') {
    if (key && URL_VALUED_KEYS.has(normalizeKey(key))) return redactUrl(value)
    return redactFreeText(value)
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
      redacted[childKey] = redactValue(childValue, childKey)
    }
    return redacted
  }

  return value
}
