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
  if (event.event === '$snapshot') redactSnapshotUrls(event.properties)
  return event
}

function redactProperties(properties: Record<string, unknown> | undefined): void {
  if (!properties) return
  for (const key of Object.keys(properties)) {
    const value = properties[key]
    if (typeof value !== 'string') continue
    // Path ids are scrubbed from EVERY string, not just from an allowlist of
    // URL-valued keys: the capability travels in $current_url, $pathname,
    // $referrer, $initial_current_url and $elements_chain, and an allowlist
    // goes stale the next time posthog-js adds a URL property.
    properties[key] = isSensitiveKey(key) ? REDACTED : redactFreeText(value)
  }
}

/**
 * Scrubs every URL-bearing string inside a session-replay snapshot batch, which
 * `redactProperties` cannot reach: they are nested in the `$snapshot_data` array,
 * and the recorder reads `window.location.href` itself rather than the
 * already-scrubbed `$current_url`.
 *
 * The whole payload is walked rather than the two known offsets (the Meta href
 * that becomes a recording's `first_url`, and the `$pageview` custom event's
 * payload). rrweb full snapshots (type 2) carry a SERIALIZED DOM whose anchors
 * keep `href="/mentor/requests/<uuid>"`, and incremental mutations (type 3) carry
 * the same hrefs under `adds[].node` and `attributes[].attributes` — so
 * inspecting only types 4 and 5 exported the capability with every replay.
 *
 * Cost: the walk is iterative, so a deep DOM cannot overflow the stack inside
 * `before_send`, and every string is gated on two `indexOf`s before any regex
 * runs. The bulk of a snapshot (tag names, css property names, text) therefore
 * costs a single character scan — far less than the JSON serialization posthog-js
 * already does on the same payload.
 */
function redactSnapshotUrls(properties: Record<string, unknown> | undefined): void {
  const batch = properties?.$snapshot_data
  if (!batch || typeof batch !== 'object') return

  const pending: object[] = [batch]
  while (pending.length > 0) {
    const node = pending.pop() as Record<string, unknown>
    // Array index access goes through the same string-keyed path, so one branch
    // covers both the batch array and the node objects inside it.
    for (const key of Object.keys(node)) {
      const value = node[key]
      if (typeof value === 'string') {
        node[key] = redactSnapshotString(value)
      } else if (value && typeof value === 'object') {
        pending.push(value)
      }
    }
  }
}

function redactSnapshotString(value: string): string {
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
