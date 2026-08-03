import posthog from 'posthog-js'
import type { CaptureResult } from 'posthog-js'
import { REDACTED, isSensitiveKey, redactQueryValues } from '@/lib/redact'

let initialized = false

// SECURITY (M10, widened for P14): strip one-time tokens (magic-link/confirm
// token, review request_id) from event properties before they are sent. The
// autocaptured $current_url is the main carrier: /reviews/new and the auth
// callbacks drop their query string on mount, but the first pageview fires
// before that. The rules are shared with the server-side sinks so there is one
// list to keep current, and they now also cover login_token, confirm_token and
// the camelCase spellings the old two-name regex missed.
function redactSensitiveEvent(event: CaptureResult | null): CaptureResult | null {
  if (!event || !event.properties) return event
  for (const key of Object.keys(event.properties)) {
    const value = event.properties[key]
    if (typeof value !== 'string') continue
    event.properties[key] = isSensitiveKey(key) ? REDACTED : redactQueryValues(value)
  }
  return event
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
