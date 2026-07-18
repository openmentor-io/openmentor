import type { NextApiRequest, NextApiResponse } from 'next'
import { getGoApiClient, HttpError } from '@/lib/go-api-client'
import { logError } from '@/lib/logger'
import { withObservability } from '@/lib/with-observability'

/**
 * POST /api/mentor/auth/verify
 * Verify login token and create session (sets HttpOnly cookie)
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
    const client = getGoApiClient()
    const { data, headers } = await client.mentorVerifyLogin(token)

    // Forward Set-Cookie header(s) from Go API to browser. SECURITY (M15):
    // getSetCookie() preserves each cookie as a separate header; headers.get()
    // comma-joins them and mangles attributes (HttpOnly/Secure/SameSite).
    const setCookies = headers.getSetCookie()
    if (setCookies.length > 0) {
      res.setHeader('Set-Cookie', setCookies)
    }

    res.status(200).json(data)
  } catch (error) {
    // SECURITY (M7): forward only 4xx contracts; 5xx -> generic, no body leak.
    if (error instanceof HttpError && error.statusCode >= 400 && error.statusCode < 500) {
      try {
        res.status(error.statusCode).json(JSON.parse(error.body))
      } catch {
        res.status(error.statusCode).json({ success: false, message: error.statusText || 'Request failed' })
      }
      return
    }

    if (error instanceof Error) {
      logError(error, { context: 'mentor-verify-login', method: req.method, url: req.url })
    }
    res.status(500).json({ success: false, message: 'Internal server error' })
  }
}

export default withObservability(handler)
