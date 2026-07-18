import type { NextApiRequest, NextApiResponse } from 'next'
import { getGoApiClient, HttpError } from '@/lib/go-api-client'
import { logError } from '@/lib/logger'
import { maskEmail } from '@/lib/pii'
import { withObservability } from '@/lib/with-observability'

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

  const genericSuccessResponse = {
    success: true,
    message: 'If your email is registered, you will receive a login link',
  }

  try {
    const client = getGoApiClient()
    await client.adminRequestLogin(email)
    res.status(200).json(genericSuccessResponse)
  } catch (error) {
    if (error instanceof HttpError) {
      logError(new Error(`Admin request login failed: ${error.statusCode}`), {
        context: 'admin-request-login',
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
      logError(error, { context: 'admin-request-login', method: req.method, url: req.url })
    }
    res.status(500).json({ success: false, message: 'Internal server error' })
  }
}

export default withObservability(handler)
