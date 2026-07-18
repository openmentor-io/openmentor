import type { NextApiRequest, NextApiResponse } from 'next'
import { getGoApiClient } from '@/lib/go-api-client'
import { sendUpstreamError } from '@/lib/api-proxy'
import { withObservability } from '@/lib/with-observability'
import type { ConfirmMentorEmailRequest } from '@/types'

/**
 * POST /api/mentor/confirm - Confirm a mentor's email address
 * (public /mentor/confirm page, draft-status registration flow)
 *
 * SECURITY: Next.js API proxy so the Go API stays on localhost only.
 * Error status codes (400 invalid, 410 expired) pass through so the page
 * can offer a resend for expired links.
 */
async function handler(req: NextApiRequest, res: NextApiResponse): Promise<void> {
  if (req.method !== 'POST') {
    res.status(405).json({ error: 'Method not allowed' })
    return
  }

  try {
    const client = getGoApiClient()
    const data = await client.confirmMentorEmail(req.body as ConfirmMentorEmailRequest)

    res.status(200).json(data)
  } catch (error) {
    sendUpstreamError(res, error, { context: 'mentor-confirm-proxy', method: req.method, url: req.url })
  }
}

export default withObservability(handler)
