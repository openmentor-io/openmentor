import type { NextApiRequest, NextApiResponse } from 'next'
import { getGoApiClient } from '@/lib/go-api-client'
import { sendUpstreamError } from '@/lib/api-proxy'
import { withObservability } from '@/lib/with-observability'

/**
 * POST /api/mentee/auth/logout
 * Clear the mentee session cookie and revoke every session for the mentee.
 */
async function handler(req: NextApiRequest, res: NextApiResponse): Promise<void> {
  if (req.method !== 'POST') {
    res.status(405).json({ error: 'Method not allowed' })
    return
  }

  try {
    const { data, headers } = await getGoApiClient().menteeLogout(req.headers.cookie || '')
    const setCookies = headers.getSetCookie()
    if (setCookies.length > 0) {
      res.setHeader('Set-Cookie', setCookies)
    }
    res.status(200).json(data)
  } catch (error) {
    sendUpstreamError(res, error, { context: 'mentee-logout', method: req.method })
  }
}

export default withObservability('/api/mentee/auth/logout', handler)
