import type { NextApiRequest, NextApiResponse } from 'next'
import { getGoApiClient } from '@/lib/go-api-client'
import { sendUpstreamError } from '@/lib/api-proxy'
import { withObservability } from '@/lib/with-observability'

/**
 * GET /api/username-availability?u=<candidate>
 *
 * Public proxy for the registration form's live username availability check.
 * Unauthenticated; the Go API applies its own contact-tier rate limit.
 */
async function handler(req: NextApiRequest, res: NextApiResponse): Promise<void> {
  if (req.method !== 'GET') {
    res.status(405).json({ error: 'Method not allowed' })
    return
  }

  const candidate = typeof req.query.u === 'string' ? req.query.u : ''
  if (!candidate) {
    res.status(400).json({ error: "Query parameter 'u' is required" })
    return
  }

  try {
    const client = getGoApiClient()
    const data = await client.usernameAvailability(candidate)
    res.status(200).json(data)
  } catch (error) {
    sendUpstreamError(res, error, { context: 'username-availability', method: req.method, url: req.url })
  }
}

export default withObservability(handler)
