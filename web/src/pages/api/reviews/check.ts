import type { NextApiRequest, NextApiResponse } from 'next'
import { getGoApiClient } from '@/lib/go-api-client'
import { sendUpstreamError } from '@/lib/api-proxy'
import { withObservability } from '@/lib/with-observability'

interface ReviewCheckResponse {
  canSubmit: boolean
  error?: string
  mentorName?: string
}

interface ErrorResponse {
  error: string
}

interface CheckRequestBody {
  /** H4 review capability. Body-only — never a query parameter. */
  token?: string
  /** Legacy client_requests.id link, accepted during the dual-read window. */
  requestId?: string
}

/**
 * API route to check whether a review can still be submitted.
 *
 * SECURITY (H4): a POST, not a GET, because the capability travels in the body.
 * A query parameter would reach this route's own access log, `$current_url` on
 * the client, and the Go API's `url.query` span attribute — which is exactly how
 * 12 request ids ended up in PostHog with a 12-month retention window.
 */
async function handler(
  req: NextApiRequest,
  res: NextApiResponse<ReviewCheckResponse | ErrorResponse>
): Promise<void> {
  if (req.method !== 'POST') {
    res.status(405).json({ error: 'Method not allowed' })
    return
  }

  try {
    const { token, requestId } = (req.body ?? {}) as CheckRequestBody
    const client = getGoApiClient()

    if (typeof token === 'string' && token) {
      const result = await client.request<ReviewCheckResponse>('POST', '/api/v1/reviews/check', {
        body: { token },
      })
      res.status(200).json(result)
      return
    }

    // Legacy path: the request id IS the capability. Removed at the H4 cutover.
    if (typeof requestId === 'string' && requestId) {
      const result = await client.request<ReviewCheckResponse>(
        'GET',
        `/api/v1/reviews/${encodeURIComponent(requestId)}/check`
      )
      res.status(200).json(result)
      return
    }

    res.status(400).json({ error: 'Invalid review link' })
  } catch (error) {
    sendUpstreamError(res, error, {
      context: 'check-review-proxy',
      method: req.method,
      // req.url is a constant here: the capability is in the body on both paths,
      // so nothing capability-bearing reaches this log field.
      url: req.url,
    })
  }
}

export default withObservability('/api/reviews/check', handler)
