import posthog from 'posthog-js'
import type { CaptureResult } from 'posthog-js'
import { REDACTED, isSensitiveKey, redactFreeText } from '@/lib/redact'

let initialized = false

// SECURITY (M10, widened for P14): strip one-time tokens (magic-link/confirm
// token, review request_id) from event properties before they are sent. The
// autocaptured URL is the main carrier: /reviews/new and the auth callbacks drop
// their query string on mount, but the first pageview fires before that, and
// /mentor/requests/<uuid> carries the capability in the path for the whole
// visit. The rules are shared with the server-side sinks so there is one list.
export function redactSensitiveEvent(event: CaptureResult | null): CaptureResult | null {
  if (!event) return event
  redactProperties(event.properties)
  // $set / $set_once are TOP-LEVEL siblings of properties, and posthog-js puts
  // $initial_current_url in $set_once on $identify. A capability landing there
  // becomes a PERSON property, which outlives the event that carried it.
  redactProperties(event.$set)
  redactProperties(event.$set_once)
  return event
}

function redactProperties(properties: Record<string, unknown> | undefined): void {
  if (!properties) return
  for (const key of Object.keys(properties)) {
    const value = properties[key]
    if (typeof value === 'string') {
      properties[key] = redactPropertyString(key, value)
    } else if (value && typeof value === 'object') {
      // Nested values need the same rules: autocapture puts the clicked anchor's
      // `attr__href` in the `$elements` ARRAY, and session replay puts serialized
      // DOM hrefs in `$snapshot_data`. Neither is a top-level string, so the
      // branch above cannot see either one.
      redactNested(value)
    }
  }
}

function redactPropertyString(key: string, value: string): string {
  if (isProjectApiKey(key, value)) return value
  // Path ids are scrubbed from EVERY string, not just from an allowlist of
  // URL-valued keys: the capability travels in $current_url, $pathname,
  // $referrer, $initial_current_url and $elements_chain, and an allowlist
  // goes stale the next time posthog-js adds a URL property.
  return isSensitiveKey(key) ? REDACTED : redactFreeText(value)
}

/**
 * True for the property posthog-js uses to authenticate the request itself.
 * `properties.token` is where the SDK puts the project key, and it derives the
 * capture body's `api_key` from exactly that field — so the `token` key rule
 * would have rewritten it to `[REDACTED]` and PostHog would have rejected every
 * batch. That is an analytics outage, not extra safety (see D52).
 *
 * Exempted by VALUE, not by key name: only the configured public project key
 * passes, so any other `token` property is still treated as a credential. The
 * key is public by construction — it ships in the client bundle.
 */
function isProjectApiKey(key: string, value: string): boolean {
  return key === 'token' && !!value && value === process.env.NEXT_PUBLIC_POSTHOG_KEY
}

/**
 * Applies the property rules to every string nested inside an event property.
 *
 * The whole payload is walked rather than known offsets. rrweb full snapshots
 * (type 2) carry a SERIALIZED DOM whose anchors keep
 * `href="/mentor/requests/<uuid>"`, incremental mutations (type 3) carry the same
 * hrefs under `adds[].node` and `attributes[].attributes`, and `$autocapture`
 * carries them as `attr__href` on every entry of `$elements` — so inspecting
 * only the Meta and Custom snapshot events exported the capability with every
 * replay AND with every click on a request link.
 *
 * Cost: the walk is iterative, so a deep DOM cannot overflow the stack inside
 * `before_send`, and every string is gated on two `indexOf`s before any regex
 * runs. The bulk of a snapshot (tag names, css property names, text) therefore
 * costs a single character scan — far less than the JSON serialization posthog-js
 * already does on the same payload.
 */
function redactNested(root: object): void {
  // The walk reaches arbitrary caller-supplied properties now, not only the
  // JSON-shaped snapshot batch, so a cycle has to end the traversal instead of
  // spinning inside before_send and freezing the tab.
  const seen = new WeakSet<object>([root])
  const pending: object[] = [root]
  while (pending.length > 0) {
    const node = pending.pop() as Record<string, unknown>
    // Array index access goes through the same string-keyed path, so one branch
    // covers both the arrays and the objects inside them.
    for (const key of Object.keys(node)) {
      const value = node[key]
      if (typeof value === 'string') {
        node[key] = redactNestedString(key, value)
      } else if (value && typeof value === 'object' && !seen.has(value)) {
        seen.add(value)
        pending.push(value)
      }
    }
  }
}

function redactNestedString(key: string, value: string): string {
  if (isSensitiveKey(key)) return REDACTED
  // A data: URI is a base64 blob, not a URL: the `key=value` rule can bite on its
  // `=` padding and corrupt the asset in playback, and it cannot carry a
  // capability path.
  if (value.startsWith('data:')) return value
  // Neither rule can match without one of these two characters.
  if (!value.includes('/') && !value.includes('=')) return value
  return redactFreeText(value)
}

export function initializePostHog(): typeof posthog | null {
  if (initialized || typeof window === 'undefined') {
    return typeof window !== 'undefined' ? posthog : null
  }

  const apiKey = process.env.NEXT_PUBLIC_POSTHOG_KEY
  const apiHost = process.env.NEXT_PUBLIC_POSTHOG_HOST

  if (!apiKey || !apiHost) {
    console.info(
      '[PostHog] Skipping initialization - NEXT_PUBLIC_POSTHOG_KEY or NEXT_PUBLIC_POSTHOG_HOST not configured'
    )
    return null
  }

  posthog.init(apiKey, {
    api_host: apiHost,
    ui_host: 'https://eu.posthog.com',

    // SECURITY (M10): redact one-time tokens from captured URLs.
    before_send: redactSensitiveEvent,

    // Explicitly set to true — posthog-js v1.359+ defaults to "history_change"
    // when no `defaults` date is provided (string "unset" >= "2025-05-24"),
    // which skips the initial page load $pageview and breaks web metrics.
    capture_pageview: true,
    capture_pageleave: true,

    // Error tracking — auto-capture unhandled errors and promise rejections
    capture_exceptions: true,

    // heatmaps
    enable_heatmaps: true,
  })

  initialized = true
  return posthog
}

export function getPostHogClient(): typeof posthog | null {
  if (typeof window === 'undefined') return null
  return initialized ? posthog : null
}

export function captureException(error: Error, context?: Record<string, string>): void {
  if (typeof window !== 'undefined' && initialized) {
    posthog.captureException(error, context)
  }
}
