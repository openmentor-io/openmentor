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
  if (!cookies) {
    res.status(401).json({ error: 'Unauthorized' })
    return
  }

  const { id } = req.query
  const mentorId = Array.isArray(id) ? id[0] : id
  if (!mentorId || typeof mentorId !== 'string') {
    res.status(400).json({ error: 'Invalid mentor ID' })
    return
  }

  const imageData = req.body
  if (!imageData || typeof imageData.image !== 'string') {
    res.status(400).json({ error: 'Invalid image data' })
    return
  }

  try {
    const client = getGoApiClient()
    const data = await client.adminUploadMentorPicture(cookies, mentorId, imageData)
    res.status(200).json(data)
  } catch (error) {
    sendUpstreamError(res, error, { context: 'admin-upload-mentor-picture', method: req.method, url: req.url })
  }
}

export default withObservability(handler)

// Match backend image validation limit (10 MB).
export const config = {
  api: {
    bodyParser: {
      sizeLimit: '10mb',
    },
  },
}
