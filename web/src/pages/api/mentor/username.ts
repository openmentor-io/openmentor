import type { NextApiRequest, NextApiResponse } from 'next'
import { getGoApiClient } from '@/lib/go-api-client'
import { sendUpstreamError } from '@/lib/api-proxy'
import { withObservability } from '@/lib/with-observability'

/**
 * GET  /api/mentor/username                  - current username + cooldown state
 * GET  /api/mentor/username?u=<candidate>    - availability check for a candidate
 * POST /api/mentor/username  { username }    - change the mentor's own username
 *
 * Requires session cookie authentication. Changing a username is a breaking
 * action (shared links, cached OG cards) so it lives on its own endpoint,
 * separate from the regular profile-save route, with a 14-day cooldown.
 */
async function handler(req: NextApiRequest, res: NextApiResponse): Promise<void> {
  const cookies = req.headers.cookie
  if (!cookies) {
    res.status(401).json({ error: 'Unauthorized' })
    return
  }

  const client = getGoApiClient()

  try {
    if (req.method === 'GET') {
      const candidate = typeof req.query.u === 'string' ? req.query.u : ''
      if (candidate) {
        const data = await client.mentorUsernameAvailability(cookies, candidate)
        res.status(200).json(data)
        return
      }
      const data = await client.mentorGetUsernameStatus(cookies)
      res.status(200).json(data)
      return
    }

    if (req.method === 'POST') {
      const { username } = req.body ?? {}
      if (typeof username !== 'string' || username.trim() === '') {
        res.status(400).json({ error: 'Username is required' })
        return
      }
      const data = await client.mentorChangeUsername(cookies, username)
      res.status(200).json(data)
      return
    }

    res.status(405).json({ error: 'Method not allowed' })
  } catch (error) {
    sendUpstreamError(res, error, { context: 'mentor-username', method: req.method, url: req.url })
  }
}

export default withObservability(handler)
