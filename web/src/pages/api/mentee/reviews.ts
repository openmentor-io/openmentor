import type { NextApiRequest, NextApiResponse } from 'next'
import { getGoApiClient } from '@/lib/go-api-client'
import { sendUpstreamError } from '@/lib/api-proxy'
import { withObservability } from '@/lib/with-observability'

const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i

/**
 * POST /api/mentee/reviews
 * Leave a review on a completed request from the mentee dashboard. The session
 * cookie authorizes it; the request id stays in the body on both hops.
 */
async function handler(req: NextApiRequest, res: NextApiResponse): Promise<void> {
  if (req.method !== 'POST') {
    res.status(405).json({ error: 'Method not allowed' })
    return
  }

  const cookies = req.headers.cookie
  if (!cookies) {
    res.status(401).json({ error: 'Unauthorized' })
    return
  }

  const { requestId, mentorReview, platformReview, improvements, captchaToken } = req.body ?? {}
  if (typeof requestId !== 'string' || !UUID_RE.test(requestId)) {
    res.status(400).json({ error: 'Invalid request' })
    return
  }
  if (typeof mentorReview !== 'string' || mentorReview.trim().length < 10) {
    res.status(400).json({ error: 'The review must be at least 10 characters long' })
    return
  }
  if (mentorReview.length > 5000) {
    res.status(400).json({ error: 'Review text is too long (max 5000 characters)' })
    return
  }
  if (typeof captchaToken !== 'string' || !captchaToken) {
    res.status(400).json({ error: 'Captcha token is required' })
    return
  }

  try {
    const result = await getGoApiClient().menteeSubmitReview(cookies, {
      requestId,
      mentorReview: mentorReview.trim(),
      platformReview: typeof platformReview === 'string' ? platformReview.trim() : '',
      improvements: typeof improvements === 'string' ? improvements.trim() : '',
      captchaToken,
    })
    res.status(200).json(result)
  } catch (error) {
    sendUpstreamError(res, error, { context: 'mentee-submit-review', method: req.method })
  }
}

export default withObservability('/api/mentee/reviews', handler)
