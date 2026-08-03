/**
 * Simplified observability instrumentation for frontend Next.js application
 * Tracks HTTP requests, duration, and errors for remaining API routes
 */

import type { NextApiRequest, NextApiResponse } from 'next'
import type { Histogram } from 'prom-client'
import { httpRequestDuration, httpRequestTotal, activeRequests } from './metrics'
import { logHttpRequest, logError } from './logger'

type NextApiHandler = (req: NextApiRequest, res: NextApiResponse) => Promise<void> | void

/**
 * Normalize dynamic route segments to prevent cardinality explosion in metrics.
 * Converts actual IDs to route templates (e.g., /api/mentor/requests/rec123 -> /api/mentor/requests/:id)
 *
 * Every id-bearing segment MUST be normalized: http_route labels a counter, a
 * histogram and a gauge, so one raw id per request means an unbounded series
 * per request. This used to enumerate routes one at a time, which silently
 * missed nested ids — /api/admin/mentors/:id/requests/<requestId> kept its
 * second UUID. Matching UUID segments at any depth is what keeps it bounded as
 * routes are added.
 */
export function normalizeRoute(url: string): string {
  const path = url.split('?')[0]

  // PostgreSQL UUID (8-4-4-4-12 hex) occupying a whole path segment, anywhere.
  const normalized = path.replace(
    /\/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}(?=\/|$)/gi,
    '/:id'
  )

  // Public mentor profile pages (/mentor/<slug>) only. Scoped away from /api/*
  // because the slug pattern also matches fixed segments: it was rewriting
  // /api/mentor/requests/:id into the nonsense label /api/mentor/:slug/:id.
  const withSlug = normalized.startsWith('/api/')
    ? normalized
    : normalized.replace(/\/mentor\/[a-z0-9-]+(?:\/|$)/, '/mentor/:slug/')

  // Remove trailing slash for consistency
  return withSlug.replace(/\/$/, '') || 'unknown'
}

/**
 * Higher-order function that wraps API routes with observability instrumentation
 */
export function withObservability(handler: NextApiHandler): NextApiHandler {
  return async (req: NextApiRequest, res: NextApiResponse): Promise<void> => {
    const start = Date.now()
    const route = normalizeRoute(req.url || '')
    const method = req.method || 'UNKNOWN'

    // Track active requests
    activeRequests.inc({ http_request_method: method, http_route: route })

    // Patch res.end to capture status code and duration
    const originalEnd = res.end.bind(res)

    res.end = ((...args: Parameters<typeof originalEnd>) => {
      const statusCode = res.statusCode
      const duration = (Date.now() - start) / 1000 // Convert to seconds

      // Record metrics
      httpRequestDuration.observe(
        { http_request_method: method, http_route: route, http_response_status_code: statusCode },
        duration
      )
      httpRequestTotal.inc({
        http_request_method: method,
        http_route: route,
        http_response_status_code: statusCode,
      })
      activeRequests.dec({ http_request_method: method, http_route: route })

      // Log the normalized route, never the raw url — it carries the review
      // request_id and the magic-link token in its query string (P14).
      logHttpRequest(req, res, duration * 1000, route) // Convert back to ms for logging

      return originalEnd(...args)
    }) as NextApiResponse['end']

    try {
      // Call the actual handler
      await handler(req, res)
    } catch (error) {
      // Log the error
      if (error instanceof Error) {
        logError(error, {
          method,
          route,
          url: req.url,
        })
      }

      // Re-throw to let Next.js handle it
      throw error
    }
  }
}

interface MetricLabels {
  [key: string]: string | number
}

/**
 * Helper to measure async operations with metrics
 */
export async function measureAsync<T>(
  metric: Histogram<string>,
  labels: MetricLabels,
  operation: () => Promise<T>
): Promise<T> {
  const end = metric.startTimer(labels)
  try {
    const result = await operation()
    end()
    return result
  } catch (error) {
    end()
    throw error
  }
}

/**
 * Helper to measure sync operations with metrics
 */
export function measureSync<T>(
  metric: Histogram<string>,
  labels: MetricLabels,
  operation: () => T
): T {
  const end = metric.startTimer(labels)
  try {
    const result = operation()
    end()
    return result
  } catch (error) {
    end()
    throw error
  }
}
