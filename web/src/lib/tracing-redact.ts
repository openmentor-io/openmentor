/**
 * Span-level containment for capability-bearing URLs.
 *
 * SECURITY (P14): @opentelemetry/instrumentation-http and -undici record
 * `url.query` / `url.full` unconditionally, so a magic-link callback
 * (`/mentor/auth/callback?token=<jwt>`) or a review check
 * (`/api/reviews/check?request_id=<uuid>`) ships the credential to the trace
 * store as a span attribute. Nothing in the SDK filters that, so this does.
 */

import type { ExportResult } from '@opentelemetry/core'
import type { ReadableSpan, SpanExporter } from '@opentelemetry/sdk-trace-base'
import { REDACTED, isSensitiveKey, redactQueryValues, redactUrl } from './redact'

/**
 * Request paths whose spans are dropped outright. These pages exist ONLY to
 * consume a one-time login token, so there is no useful trace to keep and no
 * reason to hold a span that was built from the token's URL.
 */
export const IGNORED_TRACE_PATHS = ['/mentor/auth/callback', '/admin/auth/callback'] as const

/** Span attributes that hold a URL and need the query-string treatment. */
const URL_ATTRIBUTE_KEYS = [
  'url.full',
  'url.path',
  'url.query',
  'url.original',
  'http.url',
  'http.target',
  'http.route',
] as const

/**
 * Matches the auth-callback pages including Next's data route for them
 * (`/_next/data/<buildId>/mentor/auth/callback.json?token=...`).
 */
export function shouldIgnoreIncomingRequest(url: string | undefined): boolean {
  if (!url) return false
  const path = url.split('?')[0]
  return IGNORED_TRACE_PATHS.some((ignored) => path.includes(ignored))
}

/**
 * Rewrites a span's attributes in place. In-place is deliberate: ReadableSpan is
 * the SDK's live Span object, and the exporter serializes this same reference.
 */
export function redactSpanAttributes(attributes: Record<string, unknown>): void {
  for (const key of URL_ATTRIBUTE_KEYS) {
    const value = attributes[key]
    if (typeof value !== 'string') continue
    attributes[key] = key === 'url.query' ? redactQueryValues(value) : redactUrl(value)
  }

  for (const [key, value] of Object.entries(attributes)) {
    if (typeof value === 'string' && isSensitiveKey(key)) {
      attributes[key] = REDACTED
    }
  }
}

/**
 * Wraps the OTLP exporter instead of registering a SpanProcessor: NodeSDK's
 * `spanProcessors` option REPLACES the batch processor it builds from
 * `traceExporter`, so using it would mean importing BatchSpanProcessor from a
 * package we only depend on transitively. Wrapping the exporter keeps the
 * batching config as-is and still sees every span, whenever its attributes were
 * set.
 */
export class RedactingSpanExporter implements SpanExporter {
  constructor(private readonly delegate: SpanExporter) {}

  export(spans: ReadableSpan[], resultCallback: (result: ExportResult) => void): void {
    for (const span of spans) {
      redactSpanAttributes(span.attributes as Record<string, unknown>)
      if (span.name.includes('?')) {
        // Span names are usually "GET /route", but an instrumentation that puts
        // the full target in the name would carry the query string with it.
        ;(span as { name: string }).name = redactQueryValues(span.name)
      }
    }
    this.delegate.export(spans, resultCallback)
  }

  shutdown(): Promise<void> {
    return this.delegate.shutdown()
  }

  forceFlush(): Promise<void> {
    return this.delegate.forceFlush?.() ?? Promise.resolve()
  }
}
