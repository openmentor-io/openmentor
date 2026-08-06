import { createMocks } from 'node-mocks-http'
import type { NextApiRequest, NextApiResponse } from 'next'

// Mock the dependencies before importing handler
jest.mock('@/lib/logger', () => ({
  info: jest.fn(),
  warn: jest.fn(),
  error: jest.fn(),
  logError: jest.fn(),
  logHttpRequest: jest.fn(),
}))

const mockRequest = jest.fn()

jest.mock('@/lib/go-api-client', () => {
  // Keep the real HttpError so sendUpstreamError's instanceof check works.
  const actual = jest.requireActual('@/lib/go-api-client')
  return {
    HttpError: actual.HttpError,
    getGoApiClient: () => ({
      request: mockRequest,
    }),
  }
})

// Import handler after mocks are set up
import handler from '@/pages/api/reviews/submit'

const REQUEST_ID = '1b0c9e42-a072-47ed-8ce9-edd306874ec9'
// Fabricated, shaped like a real H4 capability. Never a live token.
const REVIEW_TOKEN = 'rvw_SENTINELbffSubmitCapabilityAAAAAAAAAAAAAAAA'

function validBody(): Record<string, string> {
  return {
    token: REVIEW_TOKEN,
    mentorReview: 'Great session, very practical advice.',
    captchaToken: 'valid-token',
  }
}

function legacyBody(): Record<string, string> {
  return {
    requestId: REQUEST_ID,
    mentorReview: 'Great session, very practical advice.',
    captchaToken: 'valid-token',
  }
}

describe('api/reviews/submit', () => {
  beforeEach(() => {
    jest.clearAllMocks()
  })

  it('returns 405 for non-POST requests', async () => {
    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({ method: 'GET' })

    await handler(req, res)

    expect(res.statusCode).toBe(405)
    expect(res._getJSONData()).toEqual({ error: 'Method not allowed' })
  })

  // SECURITY (H4): the capability goes upstream in the BODY, to a CONSTANT URL.
  it('forwards the token in the body to a constant upstream URL', async () => {
    mockRequest.mockResolvedValue({ success: true, reviewId: 'rev-1' })

    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'POST',
      body: validBody(),
    })

    await handler(req, res)

    expect(mockRequest).toHaveBeenCalledWith('POST', '/api/v1/reviews/submit', {
      body: {
        token: REVIEW_TOKEN,
        mentorReview: 'Great session, very practical advice.',
        platformReview: '',
        improvements: '',
        captchaToken: 'valid-token',
      },
    })
    const [, upstreamPath] = mockRequest.mock.calls[0]
    expect(upstreamPath).not.toContain(REVIEW_TOKEN)
    expect(res.statusCode).toBe(200)
    expect(res._getJSONData()).toEqual({ success: true, reviewId: 'rev-1' })
  })

  // Dual-read: a link already in a mentee's inbox still submits.
  it('still forwards a legacy request id to the legacy endpoint', async () => {
    mockRequest.mockResolvedValue({ success: true, reviewId: 'rev-1' })

    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'POST',
      body: legacyBody(),
    })

    await handler(req, res)

    expect(mockRequest).toHaveBeenCalledWith('POST', `/api/v1/reviews/${REQUEST_ID}`, {
      body: {
        mentorReview: 'Great session, very practical advice.',
        platformReview: '',
        improvements: '',
        captchaToken: 'valid-token',
      },
    })
    expect(res.statusCode).toBe(200)
  })

  it('rejects a submission with neither a token nor a request id', async () => {
    const body = validBody()
    delete body.token
    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({ method: 'POST', body })

    await handler(req, res)

    expect(mockRequest).not.toHaveBeenCalled()
    expect(res.statusCode).toBe(400)
  })

  it('rejects a missing review text locally', async () => {
    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'POST',
      body: { ...validBody(), mentorReview: '   ' },
    })

    await handler(req, res)

    expect(mockRequest).not.toHaveBeenCalled()
    expect(res.statusCode).toBe(400)
  })

  // The page shows the upstream `error` verbatim, so a spent captcha token or an
  // already-reviewed request must keep its status and message instead of
  // becoming "Failed to submit the review".
  it.each([
    [400, 'Bad Request', { error: 'Captcha verification failed' }],
    [409, 'Conflict', { error: 'A review has already been submitted for this request' }],
    [429, 'Too Many Requests', { error: 'Too many requests, please try again later' }],
  ])('forwards an upstream %i to the client', async (status, statusText, body) => {
    const { HttpError } = jest.requireActual('@/lib/go-api-client')
    mockRequest.mockRejectedValue(new HttpError(status, statusText, JSON.stringify(body)))

    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'POST',
      body: validBody(),
    })

    await handler(req, res)

    expect(res.statusCode).toBe(status)
    expect(res._getJSONData()).toEqual(body)
  })

  it('hides an upstream 5xx behind a generic 500', async () => {
    const { HttpError } = jest.requireActual('@/lib/go-api-client')
    mockRequest.mockRejectedValue(
      new HttpError(500, 'Internal Server Error', 'pq: duplicate key value violates "reviews_pkey"')
    )

    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'POST',
      body: validBody(),
    })

    await handler(req, res)

    expect(res.statusCode).toBe(500)
    expect(res._getJSONData()).toEqual({ error: 'Internal server error' })
  })

  it('returns a generic 500 for a transport failure', async () => {
    mockRequest.mockRejectedValue(new Error('fetch failed'))

    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'POST',
      body: validBody(),
    })

    await handler(req, res)

    expect(res.statusCode).toBe(500)
    expect(res._getJSONData()).toEqual({ error: 'Internal server error' })
  })
})
