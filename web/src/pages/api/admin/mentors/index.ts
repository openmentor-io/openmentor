import type { NextApiRequest, NextApiResponse } from 'next'
import { getGoApiClient } from '@/lib/go-api-client'
import { sendUpstreamError } from '@/lib/api-proxy'
import { withObservability } from '@/lib/with-observability'
import type { MentorModerationFilter } from '@/types'

const ALLOWED_STATUSES: MentorModerationFilter[] = ['pending', 'approved', 'declined']

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

  const status = (req.query.status as string) || 'pending'
  if (!ALLOWED_STATUSES.includes(status as MentorModerationFilter)) {
    res.status(400).json({ error: 'Invalid status filter' })
    return
  }

  try {
    const client = getGoApiClient()
    const data = await client.adminListMentors(cookies, status as MentorModerationFilter)
    res.status(200).json(data)
  } catch (error) {
    sendUpstreamError(res, error, { context: 'admin-list-mentors', method: req.method, url: req.url })
  }
}

export default withObservability(handler)
