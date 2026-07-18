import type { NextApiRequest, NextApiResponse } from 'next'
import { getGoApiClient, HttpError } from '@/lib/go-api-client'
import { logError } from '@/lib/logger'
import { withObservability } from '@/lib/with-observability'

/**
 * GET /api/mentor/auth/session
 * Get current mentor session
 */
async function handler(req: NextApiRequest, res: NextApiResponse): Promise<void> {
  if (req.method !== 'GET') {
    res.status(405).json({ error: 'Method not allowed' })
    return
  }

  const cookies = req.headers.cookie

  if (!cookies) {
    res.status(401).json({ success: false, error: 'Unauthorized' })
    return
  }

  try {
    const client = getGoApiClient()
    const { data } = await client.mentorGetSession(cookies)
    res.status(200).json(data)
  } catch (error) {
    // SECURITY (M7): forward only 4xx contracts; 5xx -> generic, no body leak.
    if (error instanceof HttpError && error.statusCode >= 400 && error.statusCode < 500) {
      try {
        res.status(error.statusCode).json(JSON.parse(error.body))
      } catch {
        res.status(error.statusCode).json({ success: false, error: error.statusText || 'Request failed' })
      }
      return
    }

    if (error instanceof Error) {
      logError(error, { context: 'mentor-session', method: req.method, url: req.url })
    }
    res.status(500).json({ success: false, error: 'Internal server error' })
  }
}

export default withObservability(handler)
