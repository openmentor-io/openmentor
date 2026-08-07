import type { NextApiRequest, NextApiResponse } from 'next'
import { getGoApiClient } from '@/lib/go-api-client'
import { sendUpstreamError } from '@/lib/api-proxy'
import { withObservability } from '@/lib/with-observability'

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

  const { id, requestId } = req.query
  const mentorId = Array.isArray(id) ? id[0] : id
  const clientRequestId = Array.isArray(requestId) ? requestId[0] : requestId
  if (!mentorId || typeof mentorId !== 'string') {
    res.status(400).json({ error: 'Invalid mentor ID' })
    return
  }
  if (!clientRequestId || typeof clientRequestId !== 'string') {
    res.status(400).json({ error: 'Invalid request ID' })
    return
  }

  try {
    const client = getGoApiClient()
    const request = await client.adminGetMentorRequest(cookies, mentorId, clientRequestId)
    if (!request) {
      res.status(404).json({ error: 'Request not found' })
      return
    }
    res.status(200).json({ request })
  } catch (error) {
    sendUpstreamError(res, error, {
      context: 'admin-get-mentor-request',
      method: req.method,
      url: req.url,
    })
  }
}

export default withObservability('/api/admin/mentors/:id/requests/:id', handler)
