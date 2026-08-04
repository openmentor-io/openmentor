/** Shown when the upstream gave us nothing a user can act on. */
export const GENERIC_SUBMIT_ERROR = 'Something went wrong — please try again in a minute.'

/** Long enough for a sentence, short enough not to reflow the error panel. */
const MAX_LENGTH = 200

/**
 * The message to show a user after a failed form submission.
 *
 * The API proxies forward a 4xx status and body verbatim (see
 * `sendUpstreamError`), so below 500 the `error` field is the actionable reason
 * — captcha rejected, validation, rate limit — and is worth rendering. At 500
 * and above the body is either the proxy's own generic text or, if the failure
 * happened in front of the proxy, a gateway page that may carry internals, so
 * it is never rendered.
 */
export function upstreamErrorMessage(
  status: number,
  body: unknown,
  fallback: string = GENERIC_SUBMIT_ERROR
): string {
  if (status >= 500) return fallback

  const raw = (body as { error?: unknown } | null | undefined)?.error
  if (typeof raw !== 'string') return fallback

  // Collapse whitespace: the panel is one line of prose, not a log dump.
  const message = raw.replace(/\s+/g, ' ').trim().slice(0, MAX_LENGTH)
  if (!message) return fallback

  return /[.!?]$/.test(message) ? message : `${message}.`
}
