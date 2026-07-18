import type { NextApiRequest, NextApiResponse } from 'next'
import { getGoApiClient } from '@/lib/go-api-client'
import { sendUpstreamError } from '@/lib/api-proxy'
import { withObservability } from '@/lib/with-observability'
import type { AdminStatusUpdateRequest } from '@/types'

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

  const { id } = req.query
  const mentorId = Array.isArray(id) ? id[0] : id
  if (!mentorId || typeof mentorId !== 'string') {
    res.status(400).json({ error: 'Invalid mentor ID' })
    return
  }

  const body = req.body as AdminStatusUpdateRequest
  if (!body || (body.status !== 'active' && body.status !== 'inactive')) {
    res.status(400).json({ error: 'Status must be active or inactive' })
    return
  }

  try {
    const client = getGoApiClient()
    const mentor = await client.adminUpdateMentorStatus(cookies, mentorId, body)
    res.status(200).json({ mentor })
  } catch (error) {
    sendUpstreamError(res, error, { context: 'admin-update-mentor-status', method: req.method, url: req.url })
  }
}

export default withObservability(handler)
