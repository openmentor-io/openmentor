import type { NextApiResponse } from 'next'
import { HttpError } from '@/lib/go-api-client'
import { logError } from '@/lib/logger'

/**
 * Translate an error thrown by a Go API proxy call into a client response.
 *
 * SECURITY (M7): only forward the upstream status and body for expected client
 * errors (4xx) — these are the form/validation contracts the frontend relies
 * on. Any 5xx, or a non-HttpError failure, returns a generic 500 and is logged
 * server-side only, so internal details (SQL errors, internal hostnames, stack
 * text embedded in HttpError.message) never reach the browser.
 */
export function sendUpstreamError(
  res: NextApiResponse,
  error: unknown,
  logContext: Record<string, unknown>
): void {
  if (error instanceof HttpError && error.statusCode >= 400 && error.statusCode < 500) {
    try {
      res.status(error.statusCode).json(JSON.parse(error.body))
    } catch {
      // Non-JSON upstream body: return a safe, body-free message.
      res.status(error.statusCode).json({ error: error.statusText || 'Request failed' })
    }
    return
  }

  if (error instanceof Error) {
    logError(error, logContext)
  }
  res.status(500).json({ error: 'Internal server error' })
}
