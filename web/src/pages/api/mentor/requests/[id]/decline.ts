import type { NextApiRequest, NextApiResponse } from 'next'
import { getGoApiClient } from '@/lib/go-api-client'
import { sendUpstreamError } from '@/lib/api-proxy'
import { withObservability } from '@/lib/with-observability'
import type { DeclineReasonValue } from '@/types'

const VALID_REASONS: DeclineReasonValue[] = [
  'no_time',
  'topic_mismatch',
  'helping_others',
  'on_break',
  'other',
]

/**
 * POST /api/mentor/requests/[id]/decline
 * Decline a request with reason
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

  const { id } = req.query
  if (!id || typeof id !== 'string') {
    res.status(400).json({ error: 'Invalid request ID' })
    return
  }

  const { reason, comment } = req.body
  if (!reason || !VALID_REASONS.includes(reason)) {
    res.status(400).json({ error: `Invalid reason. Must be one of: ${VALID_REASONS.join(', ')}` })
    return
  }

  try {
    const client = getGoApiClient()
    const data = await client.mentorDeclineRequest(cookies, id, { reason, comment })
    res.status(200).json(data)
  } catch (error) {
    sendUpstreamError(res, error, { context: 'mentor-decline-request', method: req.method, url: req.url })
  }
}

export default withObservability(handler)
