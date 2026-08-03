import posthog from 'posthog-js'
import type { CaptureResult } from 'posthog-js'
import { REDACTED, isSensitiveKey, redactPathIds, redactQueryValues } from '@/lib/redact'

let initialized = false

/** rrweb event types whose payload carries the page URL. */
const RRWEB_META = 4
const RRWEB_CUSTOM = 5

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
    properties[key] = isSensitiveKey(key) ? REDACTED : redactPathIds(redactQueryValues(value))
  }
}

/**
 * Scrubs the URLs inside a session-replay snapshot batch. PostHog derives a
 * recording's `first_url` from these, and they bypass redactProperties twice
 * over: they are nested inside the $snapshot_data array, and the recorder reads
 * window.location.href itself rather than the (already scrubbed) $current_url.
 */
function redactSnapshotUrls(properties: Record<string, unknown> | undefined): void {
  const batch = properties?.$snapshot_data
  if (!batch) return

  type RrwebEvent = { type?: number; data?: Record<string, unknown> } | null
  for (const entry of (Array.isArray(batch) ? batch : [batch]) as RrwebEvent[]) {
    if (!entry?.data) continue
    if (entry.type === RRWEB_META) {
      redactHref(entry.data)
    } else if (entry.type === RRWEB_CUSTOM) {
      redactHref(entry.data.payload as Record<string, unknown> | undefined)
    }
  }
}

function redactHref(holder: Record<string, unknown> | undefined): void {
  if (typeof holder?.href !== 'string') return
  holder.href = redactPathIds(redactQueryValues(holder.href))
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
