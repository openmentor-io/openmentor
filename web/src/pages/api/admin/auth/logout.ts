import type { NextApiRequest, NextApiResponse } from 'next'
import { getGoApiClient } from '@/lib/go-api-client'
import { sendUpstreamError } from '@/lib/api-proxy'
import { withObservability } from '@/lib/with-observability'

async function handler(req: NextApiRequest, res: NextApiResponse): Promise<void> {
  if (req.method !== 'POST') {
    res.status(405).json({ error: 'Method not allowed' })
    return
  }

  const cookies = req.headers.cookie

  try {
    const client = getGoApiClient()
    const { data, headers } = await client.adminLogout(cookies)

    // SECURITY (M15): getSetCookie() keeps multiple cookies as separate headers.
    const setCookies = headers.getSetCookie()
    if (setCookies.length > 0) {
      res.setHeader('Set-Cookie', setCookies)
    }

    res.status(200).json(data)
  } catch (error) {
    sendUpstreamError(res, error, { context: 'admin-logout', method: req.method, url: req.url })
  }
}

export default withObservability(handler)
