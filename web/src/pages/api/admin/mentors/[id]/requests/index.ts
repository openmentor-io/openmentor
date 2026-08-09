import type { NextApiRequest, NextApiResponse } from 'next'
import { getGoApiClient } from '@/lib/go-api-client'
import { sendUpstreamError } from '@/lib/api-proxy'
import { withObservability } from '@/lib/with-observability'
import type { RequestStatus } from '@/types'
import { ALL_REQUEST_STATUSES } from '@/types'

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

  const { id, status } = req.query
  const mentorId = Array.isArray(id) ? id[0] : id
  if (!mentorId || typeof mentorId !== 'string') {
    res.status(400).json({ error: 'Invalid mentor ID' })
    return
  }

  const statusFilter = Array.isArray(status) ? status[0] : status
  if (statusFilter && !ALL_REQUEST_STATUSES.includes(statusFilter as RequestStatus)) {
    res.status(400).json({ error: 'Invalid status filter' })
    return
  }

  try {
    const client = getGoApiClient()
    const requests = await client.adminListMentorRequests(
      cookies,
      mentorId,
      statusFilter as RequestStatus | undefined
    )
    res.status(200).json(requests)
  } catch (error) {
    sendUpstreamError(res, error, {
      context: 'admin-list-mentor-requests',
      method: req.method,
      url: req.url,
    })
  }
}

export default withObservability('/api/admin/mentors/:id/requests', handler)
