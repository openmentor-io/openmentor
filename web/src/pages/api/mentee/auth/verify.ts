import type { NextApiRequest, NextApiResponse } from 'next'
import { getGoApiClient } from '@/lib/go-api-client'
import { sendUpstreamError } from '@/lib/api-proxy'
import { withObservability } from '@/lib/with-observability'

/**
 * POST /api/mentee/auth/verify
 * Spend a mentee sign-in token and set the HttpOnly session cookie.
 */
async function handler(req: NextApiRequest, res: NextApiResponse): Promise<void> {
  if (req.method !== 'POST') {
    res.status(405).json({ error: 'Method not allowed' })
    return
  }

  const { token } = req.body
  if (!token || typeof token !== 'string') {
    res.status(400).json({ success: false, message: 'Token is required' })
    return
  }

  try {
    const { data, headers } = await getGoApiClient().menteeVerifyLogin(token)
    // SECURITY (M15): getSetCookie() keeps each cookie's attributes intact.
    const setCookies = headers.getSetCookie()
    if (setCookies.length > 0) {
      res.setHeader('Set-Cookie', setCookies)
    }
    res.status(200).json(data)
  } catch (error) {
    sendUpstreamError(res, error, { context: 'mentee-verify-login', method: req.method })
  }
}

export default withObservability('/api/mentee/auth/verify', handler)
