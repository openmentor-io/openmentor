import type { NextApiRequest, NextApiResponse } from 'next'
import { getGoApiClient } from '@/lib/go-api-client'
import { sendUpstreamError } from '@/lib/api-proxy'
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
    sendUpstreamError(res, error, { context: 'mentor-confirm-resend-proxy', method: req.method, url: req.url })
  }
}

export default withObservability(handler)
