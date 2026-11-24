import type { NextApiRequest, NextApiResponse } from 'next'
import { getGoApiClient } from '@/lib/go-api-client'
import { sendUpstreamError } from '@/lib/api-proxy'
import { withObservability } from '@/lib/with-observability'

/**
 * GET /api/mentee/auth/session
 * Current mentee session, or 401.
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
    const { data, headers } = await getGoApiClient().menteeGetSession(cookies)
    // A revoked session answers 401 with a clearing Set-Cookie; that path goes
    // through sendUpstreamError. A 200 carries none, but forward any anyway.
    const setCookies = headers.getSetCookie()
    if (setCookies.length > 0) {
      res.setHeader('Set-Cookie', setCookies)
    }
    res.status(200).json(data)
  } catch (error) {
    sendUpstreamError(res, error, { context: 'mentee-session', method: req.method })
  }
}

export default withObservability('/api/mentee/auth/session', handler)
