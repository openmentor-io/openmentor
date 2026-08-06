import type { NextApiRequest, NextApiResponse } from 'next'
import { getGoApiClient } from '@/lib/go-api-client'
import { sendUpstreamError } from '@/lib/api-proxy'
import { withObservability } from '@/lib/with-observability'

/**
 * POST /api/mentor/profile/delete - Delete the mentor's own profile (D70).
 *
 * Requires session cookie authentication. The body carries the username the
 * mentor retyped to confirm; the Go API checks it against the session's own
 * profile and answers 400 on a mismatch, 409 if it was already deleted.
 *
 * The delete revokes the mentor's sessions server-side, so every request after
 * this one gets a 401 — the client is expected to send the mentor to the
 * logged-out state rather than reload the portal.
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

  const { username } = (req.body ?? {}) as { username?: unknown }
  if (typeof username !== 'string' || username.trim() === '') {
    res.status(400).json({ error: 'A username confirmation is required' })
    return
  }

  try {
    const client = getGoApiClient()
    const data = await client.mentorDeleteProfile(cookies, { username })
    res.status(200).json(data)
  } catch (error) {
    sendUpstreamError(res, error, {
      context: 'mentor-delete-profile',
      method: req.method,
      url: req.url,
    })
  }
}

export default withObservability(handler)
