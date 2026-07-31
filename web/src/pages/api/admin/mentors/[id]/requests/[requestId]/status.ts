import type { NextApiRequest, NextApiResponse } from 'next'
import { getGoApiClient } from '@/lib/go-api-client'
import { sendUpstreamError } from '@/lib/api-proxy'
import { withObservability } from '@/lib/with-observability'
import type { AdminRequestStatusUpdateRequest, RequestStatus } from '@/types'
import { ALL_REQUEST_STATUSES } from '@/types'

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

  // Any known status is accepted — the admin override is allowed to move a
  // request back out of a terminal status. The workflow graph is deliberately
  // not applied here.
  const body = req.body as AdminRequestStatusUpdateRequest
  if (!body || !ALL_REQUEST_STATUSES.includes(body.status as RequestStatus)) {
    res.status(400).json({
      error: `Status must be one of: ${ALL_REQUEST_STATUSES.join(', ')}`,
    })
    return
  }

  try {
    const client = getGoApiClient()
    const request = await client.adminUpdateMentorRequestStatus(
      cookies,
      mentorId,
      clientRequestId,
      body.status
    )
    res.status(200).json({ request })
  } catch (error) {
    sendUpstreamError(res, error, {
      context: 'admin-update-mentor-request-status',
      method: req.method,
      url: req.url,
    })
  }
}

export default withObservability(handler)
