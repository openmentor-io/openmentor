import type { NextApiRequest, NextApiResponse } from 'next'
import { getGoApiClient } from '@/lib/go-api-client'
import { sendUpstreamError } from '@/lib/api-proxy'
import { withObservability } from '@/lib/with-observability'

const REASON_MAX_LENGTH = 2000

/**
 * POST /api/admin/mentors/:id/return - Return a pending mentor profile to
 * draft with a required reviewer note (moderation 'return for edits' action).
 *
 * Body: { reason: string } (required, <= 2000 chars).
 * The Go API answers 409 when the mentor has ever been activated.
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
  const mentorId = Array.isArray(id) ? id[0] : id
  if (!mentorId || typeof mentorId !== 'string') {
    res.status(400).json({ error: 'Invalid mentor ID' })
    return
  }

  const { reason } = req.body ?? {}
  if (typeof reason !== 'string' || !reason.trim() || reason.length > REASON_MAX_LENGTH) {
    res.status(400).json({ error: 'A reason (up to 2000 characters) is required' })
    return
  }

  try {
    const client = getGoApiClient()
    const mentor = await client.adminReturnMentor(cookies, mentorId, { reason })
    res.status(200).json({ mentor })
  } catch (error) {
    sendUpstreamError(res, error, { context: 'admin-return-mentor', method: req.method, url: req.url })
  }
}

export default withObservability(handler)
