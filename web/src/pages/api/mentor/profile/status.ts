import type { NextApiRequest, NextApiResponse } from 'next'
import { getGoApiClient } from '@/lib/go-api-client'
import { sendUpstreamError } from '@/lib/api-proxy'
import { withObservability } from '@/lib/with-observability'

/**
 * POST /api/mentor/profile/status - Update mentor's own profile visibility status
 *
 * Requires session cookie authentication.
 * Body: { status: 'active' | 'inactive' }
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

  const { status } = req.body ?? {}
  if (status !== 'active' && status !== 'inactive') {
    res.status(400).json({ error: 'Invalid status' })
    return
  }

  try {
    const client = getGoApiClient()
    const data = await client.mentorUpdateProfileStatus(cookies, { status })
    res.status(200).json(data)
  } catch (error) {
    sendUpstreamError(res, error, { context: 'mentor-update-profile-status', method: req.method, url: req.url })
  }
}

export default withObservability(handler)
