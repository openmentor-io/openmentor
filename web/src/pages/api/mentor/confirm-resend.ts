import type { NextApiRequest, NextApiResponse } from 'next'
import { getGoApiClient, HttpError } from '@/lib/go-api-client'
import { logError } from '@/lib/logger'
import { withObservability } from '@/lib/with-observability'
import type { ConfirmMentorEmailRequest } from '@/types'

/**
 * POST /api/mentor/confirm-resend - Re-send the confirmation email for an
 * expired confirmation token (public /mentor/confirm page)
 *
 * SECURITY: Next.js API proxy so the Go API stays on localhost only.
 */
async function handler(req: NextApiRequest, res: NextApiResponse): Promise<void> {
  if (req.method !== 'POST') {
    res.status(405).json({ error: 'Method not allowed' })
    return
  }

  try {
    const client = getGoApiClient()
    const data = await client.resendMentorConfirmation(req.body as ConfirmMentorEmailRequest)

    res.status(200).json(data)
  } catch (error) {
    if (error instanceof HttpError) {
      const statusCode = error.statusCode >= 400 && error.statusCode < 600 ? error.statusCode : 500
      try {
        const errorData = JSON.parse(error.body)
        res.status(statusCode).json(errorData)
      } catch {
        res.status(statusCode).json({ success: false, error: 'Resend failed' })
      }
      return
    }

    if (error instanceof Error) {
      logError(error, { context: 'mentor-confirm-resend-proxy', method: req.method, url: req.url })
    }
    res.status(500).json({ error: 'Internal server error' })
  }
}

export default withObservability(handler)
