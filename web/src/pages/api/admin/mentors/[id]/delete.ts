import type { NextApiRequest, NextApiResponse } from 'next'
import { getGoApiClient } from '@/lib/go-api-client'
import { sendUpstreamError } from '@/lib/api-proxy'
import { withObservability } from '@/lib/with-observability'

const USERNAME_MAX_LENGTH = 64

/**
 * POST /api/admin/mentors/:id/delete - Delete a mentor's profile (D70).
 *
 * Body: { username: string } — the TARGET profile's username, retyped by the
 * admin. The Go API enforces the admin role and re-checks the username against
 * the profile in the URL, answering 403 for a moderator, 400 on a mismatch and
 * 409 if it was already deleted.
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

  const { username } = req.body ?? {}
  if (typeof username !== 'string' || !username.trim() || username.length > USERNAME_MAX_LENGTH) {
    res.status(400).json({ error: 'A username confirmation is required' })
    return
  }

  try {
    const client = getGoApiClient()
    const mentor = await client.adminDeleteMentor(cookies, mentorId, { username })
    res.status(200).json({ mentor })
  } catch (error) {
    sendUpstreamError(res, error, {
      context: 'admin-delete-mentor',
      method: req.method,
      url: req.url,
    })
  }
}

export default withObservability('/api/admin/mentors/:id/delete', handler)
