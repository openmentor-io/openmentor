import type { NextApiRequest, NextApiResponse } from 'next'
import { getGoApiClient } from '@/lib/go-api-client'
import { sendUpstreamError } from '@/lib/api-proxy'
import { withObservability } from '@/lib/with-observability'

/**
 * GET /api/mentee/requests
 * Every request sent from the session's email, grouped by the API.
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

  try {
    res.status(200).json(await getGoApiClient().menteeGetRequests(cookies))
  } catch (error) {
    sendUpstreamError(res, error, { context: 'mentee-get-requests', method: req.method })
  }
}

export default withObservability('/api/mentee/requests', handler)
