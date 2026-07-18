import type { NextApiRequest, NextApiResponse } from 'next'
import { getGoApiClient } from '@/lib/go-api-client'
import { sendUpstreamError } from '@/lib/api-proxy'
import { withObservability } from '@/lib/with-observability'
import type { AdminMentorProfileUpdateRequest } from '@/types'

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

  if (req.method === 'GET') {
    try {
      const mentor = await client.adminGetMentorById(cookies, mentorId)
      if (!mentor) {
        res.status(404).json({ error: 'Mentor not found' })
        return
      }
      res.status(200).json({ mentor })
    } catch (error) {
      sendUpstreamError(res, error, { context: 'admin-get-mentor', method: req.method, url: req.url })
    }
    return
  }

  if (req.method === 'POST') {
    const body = req.body as AdminMentorProfileUpdateRequest
    if (!body || typeof body !== 'object') {
      res.status(400).json({ error: 'Invalid profile data' })
      return
    }

    try {
      const mentor = await client.adminUpdateMentor(cookies, mentorId, body)
      res.status(200).json({ mentor })
    } catch (error) {
      sendUpstreamError(res, error, { context: 'admin-update-mentor', method: req.method, url: req.url })
    }
    return
  }

  res.status(405).json({ error: 'Method not allowed' })
}

export default withObservability(handler)
