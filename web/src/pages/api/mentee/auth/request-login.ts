import type { NextApiRequest, NextApiResponse } from 'next'
import { getGoApiClient, HttpError } from '@/lib/go-api-client'
import { logError } from '@/lib/logger'
import { maskEmail } from '@/lib/pii'
import { withObservability } from '@/lib/with-observability'

/**
 * POST /api/mentee/auth/request-login
 * Email a mentee a sign-in link to their dashboard (D95).
 */
async function handler(req: NextApiRequest, res: NextApiResponse): Promise<void> {
  if (req.method !== 'POST') {
    res.status(405).json({ error: 'Method not allowed' })
    return
  }

  const { email } = req.body
  if (!email || typeof email !== 'string') {
    res.status(400).json({ success: false, message: 'Email is required' })
    return
  }

  // One answer whether or not the address has ever sent a request (M3).
  const genericSuccessResponse = {
    success: true,
    message: 'If you have sent a request from this email, you will receive a sign-in link',
  }

  try {
    await getGoApiClient().menteeRequestLogin(email)
    res.status(200).json(genericSuccessResponse)
  } catch (error) {
    if (error instanceof HttpError) {
      logError(new Error(`Mentee request login failed: ${error.statusCode}`), {
        context: 'mentee-request-login',
        email: maskEmail(email),
        statusCode: error.statusCode,
      })
      if (error.statusCode >= 400 && error.statusCode < 500) {
        res.status(200).json(genericSuccessResponse)
        return
      }
      res.status(500).json({ success: false, message: 'Internal server error' })
      return
    }
    if (error instanceof Error) {
      logError(error, { context: 'mentee-request-login', method: req.method, url: req.url })
    }
    res.status(500).json({ success: false, message: 'Internal server error' })
  }
}

export default withObservability('/api/mentee/auth/request-login', handler)
