/**
 * Simplified observability instrumentation for frontend Next.js application
 * Tracks HTTP requests, duration, and errors for remaining API routes
 */

import type { NextApiRequest, NextApiResponse } from 'next'
import type { Histogram } from 'prom-client'
import { httpRequestDuration, httpRequestTotal, activeRequests } from './metrics'
import { logHttpRequest, logError } from './logger'
import { assertApiRouteLabel, type ApiRouteLabel } from './api-routes'

type NextApiHandler = (req: NextApiRequest, res: NextApiResponse) => Promise<void> | void

/**
 * Higher-order function that wraps an API route with observability
 * instrumentation.
 *
 * `route` is the metric label, and it is a compile-time literal from
 * `API_ROUTE_LABELS` — never derived from `req.url` (C7). Two consequences:
 * `http_route` cardinality equals the number of call sites, so a flood of
 * unique unauthenticated paths mints no series at all; and the access log line
 * carries the template rather than the raw URL, which holds the review
 * `request_id` and the magic-link token in its query string (P14).
 */
export function withObservability(route: ApiRouteLabel, handler: NextApiHandler): NextApiHandler {
  // Checked at module-import time, so a bad label is a loud dev failure on the
  // first request to the route rather than a quietly mislabelled series. A JS
  // caller, or a `as never` cast, is the only way past the type.
  assertApiRouteLabel(route)

  return async (req: NextApiRequest, res: NextApiResponse): Promise<void> => {
    const start = Date.now()
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

      // The declared template, never the raw url (see the note above).
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
