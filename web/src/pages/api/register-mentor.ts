import type { NextApiRequest, NextApiResponse } from 'next'
import { getGoApiClient } from '@/lib/go-api-client'
import { sendUpstreamError } from '@/lib/api-proxy'
import { withObservability } from '@/lib/with-observability'
import type { RegisterMentorRequest } from '@/types/api'

/**
 * SECURITY: Next.js API proxy for register-mentor endpoint
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
    const data = await client.registerMentor(req.body as RegisterMentorRequest)

    res.status(200).json(data)
  } catch (error) {
    // Forward expected 4xx bodies (e.g. username_taken / username_invalid,
    // validation errors) so the form can surface them on the right field;
    // 5xx and non-HTTP failures collapse to a safe generic 500.
    sendUpstreamError(res, error, { context: 'register-mentor-proxy', method: req.method, url: req.url })
  }
}

export default withObservability(handler)

// The photo arrives base64-encoded inside JSON (FileReader.readAsDataURL), so
// this is NOT the advertised file size: it must carry MAX_IMAGE_FILE_BYTES at
// ~4/3 plus the rest of the form. Next needs a static literal here, so the value
// is duplicated from MAX_IMAGE_REQUEST_BODY in @/config/uploads and checked by
// config/__tests__/uploads.test.ts.
export const config = {
  api: {
    bodyParser: {
      sizeLimit: '14mb',
    },
  },
}
