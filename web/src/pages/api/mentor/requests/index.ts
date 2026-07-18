import type { NextApiRequest, NextApiResponse } from 'next'
import { getGoApiClient } from '@/lib/go-api-client'
import { sendUpstreamError } from '@/lib/api-proxy'
import { withObservability } from '@/lib/with-observability'

/**
 * GET /api/mentor/requests?group=active|past
 * Get mentor's requests
 */
async function handler(req: NextApiRequest, res: NextApiResponse): Promise<void> {
  if (req.method !== 'GET') {
    res.status(405).json({ error: 'Method not allowed' })
    return
  }

  const cookies = req.headers.cookie
  if (!cookies) {
    res.status(401).json({ error: 'Unauthorized' })
    return
  }

  const group = req.query.group as string
  if (!group || (group !== 'active' && group !== 'past')) {
    res.status(400).json({ error: 'Invalid group parameter. Must be "active" or "past"' })
    return
  }

  try {
    const client = getGoApiClient()
    const data = await client.mentorGetRequests(cookies, group)
    res.status(200).json(data)
  } catch (error) {
    sendUpstreamError(res, error, { context: 'mentor-get-requests', method: req.method, url: req.url })
  }
}

export default withObservability(handler)
