import type { NextApiRequest, NextApiResponse } from 'next'
import { getGoApiClient } from '@/lib/go-api-client'
import { sendUpstreamError } from '@/lib/api-proxy'
import { withObservability } from '@/lib/with-observability'
import type { ContactMentorRequest } from '@/types'

/**
 * SECURITY: Next.js API proxy for contact-mentor endpoint
 * This allows Go API to remain on localhost only (not publicly exposed)
 * Client -> Next.js API Route (this file) -> Go API (localhost)
 */
async function handler(req: NextApiRequest, res: NextApiResponse): Promise<void> {
  if (req.method !== 'POST') {
    res.status(405).json({ error: 'Method not allowed' })
    return
  }

  try {
    // Use Go API client to forward request
    const client = getGoApiClient()
    const data = await client.contactMentor(req.body as ContactMentorRequest)

    res.status(200).json(data)
  } catch (error) {
    // Forward expected 4xx bodies (captcha rejection, validation, rate limit) so
    // the form can tell the mentee what to fix instead of "something went wrong";
    // 5xx and non-HTTP failures collapse to a safe generic 500.
    sendUpstreamError(res, error, {
      context: 'contact-mentor-proxy',
      method: req.method,
      url: req.url,
    })
  }
}

export default withObservability(handler)
