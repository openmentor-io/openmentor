import type { NextApiRequest, NextApiResponse } from 'next'
import { getGoApiClient } from '@/lib/go-api-client'
import { sendUpstreamError } from '@/lib/api-proxy'
import { withObservability } from '@/lib/with-observability'

/**
 * POST /api/admin/mentors/:id/restore - Bring a deleted profile back (D70).
 *
 * The only exit from the deleted state, and admin-only (enforced by the Go
 * API, which answers 403 for a moderator and 409 if the profile is not
 * deleted). The profile comes back as 'inactive', never straight to 'active'.
 *
 * No body: unlike delete, restoring is the reversible direction and does not
 * need a typed confirmation.
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

  try {
    const client = getGoApiClient()
    const mentor = await client.adminRestoreMentor(cookies, mentorId)
    res.status(200).json({ mentor })
  } catch (error) {
    sendUpstreamError(res, error, {
      context: 'admin-restore-mentor',
      method: req.method,
      url: req.url,
    })
  }
}

export default withObservability('/api/admin/mentors/:id/restore', handler)
