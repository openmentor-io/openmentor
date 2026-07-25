import type { NextApiRequest, NextApiResponse } from 'next'
import { getGoApiClient } from '@/lib/go-api-client'
import { sendUpstreamError } from '@/lib/api-proxy'
import { withObservability } from '@/lib/with-observability'

/**
 * GET  /api/admin/mentors/[id]/username?u=<candidate> - availability check
 * POST /api/admin/mentors/[id]/username { username }  - change the username
 *
 * Admin auth (session cookie). No cooldown; goes through the same
 * history/redirect machinery as the mentor flow. The Go API enforces the
 * 'admin' role (moderators cannot rename).
 */
async function handler(req: NextApiRequest, res: NextApiResponse): Promise<void> {
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

  const client = getGoApiClient()

  try {
    if (req.method === 'GET') {
      const candidate = typeof req.query.u === 'string' ? req.query.u : ''
      if (!candidate) {
        res.status(400).json({ error: "Query parameter 'u' is required" })
        return
      }
      const data = await client.adminUsernameAvailability(cookies, mentorId, candidate)
      res.status(200).json(data)
      return
    }

    if (req.method === 'POST') {
      const { username } = req.body ?? {}
      if (typeof username !== 'string' || username.trim() === '') {
        res.status(400).json({ error: 'Username is required' })
        return
      }
      const data = await client.adminChangeUsername(cookies, mentorId, username)
      res.status(200).json(data)
      return
    }

    res.status(405).json({ error: 'Method not allowed' })
  } catch (error) {
    sendUpstreamError(res, error, { context: 'admin-mentor-username', method: req.method, url: req.url })
  }
}

export default withObservability(handler)
