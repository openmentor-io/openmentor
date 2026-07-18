import type { NextApiRequest, NextApiResponse } from 'next'
import { getGoApiClient } from '@/lib/go-api-client'
import { sendUpstreamError } from '@/lib/api-proxy'
import { withObservability } from '@/lib/with-observability'

/**
 * POST /api/mentor/profile/submit - Submit the mentor's own draft profile
 * for review (draft -> pending).
 *
 * Requires session cookie authentication. Only valid while the profile is
 * in 'draft' (the Go API answers 403 otherwise).
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

  try {
    const client = getGoApiClient()
    const data = await client.mentorSubmitProfile(cookies)
    res.status(200).json(data)
  } catch (error) {
    sendUpstreamError(res, error, { context: 'mentor-submit-profile', method: req.method, url: req.url })
  }
}

export default withObservability(handler)
