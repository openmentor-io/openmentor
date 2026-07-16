import type { NextApiRequest, NextApiResponse } from 'next'
import { getGoApiClient, HttpError } from '@/lib/go-api-client'
import { logError } from '@/lib/logger'
import { withObservability } from '@/lib/with-observability'

/**
 * POST /api/mentor/profile/submit - Submit the mentor's own draft profile
 * for review (draft -> pending).
 *
 * Requires session cookie authentication. Only valid while the profile is
 * in 'draft' (the Go API answers 403 otherwise).
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

  try {
    const client = getGoApiClient()
    const data = await client.mentorSubmitProfile(cookies)
    res.status(200).json(data)
  } catch (error) {
    if (error instanceof HttpError) {
      const statusCode = error.statusCode >= 400 && error.statusCode < 600 ? error.statusCode : 500
      try {
        const errorData = JSON.parse(error.body)
        res.status(statusCode).json(errorData)
      } catch {
        res.status(statusCode).json({ error: error.message })
      }
      return
    }

    if (error instanceof Error) {
      logError(error, { context: 'mentor-submit-profile', method: req.method, url: req.url })
    }
    res.status(500).json({ error: 'Internal server error' })
  }
}

export default withObservability(handler)
