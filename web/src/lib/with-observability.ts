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
 * Collapse id segments into route templates
 * (/api/mentor/requests/<uuid>/status -> /api/mentor/requests/:id/status).
 *
 * http_route labels a counter, a histogram and a gauge, so every id-bearing
 * segment has to collapse or one request mints three series. Matched at any
 * depth rather than route by route, so a nested id cannot be missed.
 */
export function normalizeRoute(url: string): string {
  const path = url.split('?')[0]

  // PostgreSQL UUID (8-4-4-4-12 hex) occupying a whole path segment, anywhere.
  const normalized = path.replace(
    /\/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}(?=\/|$)/gi,
    '/:id'
  )

  // Public mentor profile pages (/mentor/<slug>) only: the slug pattern also
  // matches fixed segments, so unscoped it rewrites /api/mentor/requests/:id
  // into the nonsense label /api/mentor/:slug/:id.
  const withSlug = normalized.startsWith('/api/')
    ? normalized
    : normalized.replace(/\/mentor\/[a-z0-9-]+(?:\/|$)/, '/mentor/:slug/')

  // Remove trailing slash for consistency
  return withSlug.replace(/\/$/, '') || 'unknown'
}

/**
 * Every route template that may appear as an http_route label value.
 *
 * normalizeRoute only collapses UUID-shaped segments, so a dynamic route reached
 * with any other id (an Airtable `rec…`, a numeric id) keeps it verbatim and
 * mints a series per value — and these metrics are labelled before the handler
 * authenticates anything. Mapping through this set caps the label set instead.
 *
 * A route missing from this list still works, it just aggregates into `other`.
 * Add new API routes here; `routeLabel` has a test that fails if you forget.
 */
const KNOWN_ROUTES = new Set([
  '/api/admin/auth/logout',
  '/api/admin/auth/request-login',
  '/api/admin/auth/session',
  '/api/admin/auth/verify',
  '/api/admin/mentors',
  '/api/admin/mentors/:id',
  '/api/admin/mentors/:id/approve',
  '/api/admin/mentors/:id/decline',
  '/api/admin/mentors/:id/picture',
  '/api/admin/mentors/:id/requests',
  '/api/admin/mentors/:id/requests/:id',
  '/api/admin/mentors/:id/requests/:id/status',
  '/api/admin/mentors/:id/return',
  '/api/admin/mentors/:id/status',
  '/api/admin/mentors/:id/username',
  '/api/contact-mentor',
  '/api/healthcheck',
  '/api/mentor/auth/logout',
  '/api/mentor/auth/request-login',
  '/api/mentor/auth/session',
  '/api/mentor/auth/verify',
  '/api/mentor/confirm',
  '/api/mentor/confirm-resend',
  '/api/mentor/profile',
  '/api/mentor/profile/picture',
  '/api/mentor/profile/status',
  '/api/mentor/profile/submit',
  '/api/mentor/requests',
  '/api/mentor/requests/:id',
  '/api/mentor/requests/:id/decline',
  '/api/mentor/requests/:id/status',
  '/api/mentor/username',
  '/api/register-mentor',
  '/api/reviews/check',
  '/api/reviews/submit',
  '/api/schedule-migration',
  '/api/username-availability',
])

/**
 * The http_route label value for a request URL: a known route template, or
 * `other` for anything else. Caps cardinality at len(KNOWN_ROUTES) + 1.
 */
export function routeLabel(url: string): string {
  const route = normalizeRoute(url)
  return KNOWN_ROUTES.has(route) ? route : 'other'
}

/**
 * Higher-order function that wraps API routes with observability instrumentation
 */
export function withObservability(handler: NextApiHandler): NextApiHandler {
  return async (req: NextApiRequest, res: NextApiResponse): Promise<void> => {
    const start = Date.now()
    const route = routeLabel(req.url || '')
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
