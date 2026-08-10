import type { NextApiRequest, NextApiResponse } from 'next'
import { getGoApiClient } from '@/lib/go-api-client'
import { sendUpstreamError } from '@/lib/api-proxy'
import { withObservability } from '@/lib/with-observability'

interface SubmitReviewRequest {
  /** H4 review capability. Body-only — never a path segment or query parameter. */
  token?: string
  /** Legacy client_requests.id link, accepted during the dual-read window. */
  requestId?: string
  mentorReview: string
  platformReview?: string
  improvements?: string
  captchaToken: string
}

interface SubmitReviewResponse {
  success: boolean
  reviewId?: string
  error?: string
}

interface ErrorResponse {
  error: string
  message?: string
}

/**
 * API route to submit a mentee review.
 *
 * SECURITY (H4): the capability is read from the body and forwarded in the body.
 * On the token path the upstream URL is the constant `/api/v1/reviews/submit`, so
 * no capability reaches a URL on either hop.
 */
async function handler(
  req: NextApiRequest,
  res: NextApiResponse<SubmitReviewResponse | ErrorResponse>
): Promise<void> {
  if (req.method !== 'POST') {
    res.status(405).json({ error: 'Method not allowed' })
    return
  }

  try {
    const { token, requestId, mentorReview, platformReview, improvements, captchaToken } =
      req.body as SubmitReviewRequest

    const hasToken = typeof token === 'string' && token.length > 0
    const hasRequestId = typeof requestId === 'string' && requestId.length > 0
    if (!hasToken && !hasRequestId) {
      res.status(400).json({ error: 'Invalid review link' })
      return
    }

    if (!mentorReview || typeof mentorReview !== 'string' || !mentorReview.trim()) {
      res.status(400).json({ error: 'Review text is required' })
      return
    }

    if (mentorReview.length > 5000) {
      res.status(400).json({ error: 'Review text is too long (max 5000 characters)' })
      return
    }

    if (!captchaToken || typeof captchaToken !== 'string') {
      res.status(400).json({ error: 'Captcha token is required' })
      return
    }

    const payload = {
      mentorReview: mentorReview.trim(),
      platformReview: (platformReview || '').trim(),
      improvements: (improvements || '').trim(),
      captchaToken,
    }

    const client = getGoApiClient()
    const result = hasToken
      ? await client.request<SubmitReviewResponse>('POST', '/api/v1/reviews/submit', {
          body: { ...payload, token },
        })
      : // Legacy path: the request id IS the capability, and it is in the URL
        // because that is the endpoint shape already deployed. Removed at the
        // H4 cutover; the Go middleware redacts the id out of its own telemetry.
        await client.request<SubmitReviewResponse>(
          'POST',
          `/api/v1/reviews/${encodeURIComponent(requestId as string)}`,
          { body: payload }
        )

    res.status(200).json(result)
  } catch (error) {
    // The page renders the upstream `error` field, so a spent captcha, a spent
    // token or an already-reviewed request must keep its 4xx status and message.
    sendUpstreamError(res, error, {
      context: 'submit-review-proxy',
      method: req.method,
      url: req.url,
    })
  }
}

export default withObservability('/api/reviews/submit', handler)
