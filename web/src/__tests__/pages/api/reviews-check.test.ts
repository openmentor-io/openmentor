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
import handler from '@/pages/api/reviews/check'

const REQUEST_ID = '1b0c9e42-a072-47ed-8ce9-edd306874ec9'
// Fabricated, shaped like a real H4 capability. Never a live token.
const REVIEW_TOKEN = 'rvw_SENTINELbffCheckCapabilityAAAAAAAAAAAAAAAAA'

describe('api/reviews/check', () => {
  beforeEach(() => {
    jest.clearAllMocks()
  })

  it('returns 405 for non-POST requests', async () => {
    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({ method: 'GET' })

    await handler(req, res)

    expect(res.statusCode).toBe(405)
    expect(res._getJSONData()).toEqual({ error: 'Method not allowed' })
  })

  it('returns 400 without a token or a request id', async () => {
    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({ method: 'POST', body: {} })

    await handler(req, res)

    expect(mockRequest).not.toHaveBeenCalled()
    expect(res.statusCode).toBe(400)
  })

  // SECURITY (H4): the capability must reach the Go API in the BODY. A URL would
  // put it in this route's access log, the upstream's url.path and the span.
  it('forwards the token to the Go API in the request body, never the URL', async () => {
    mockRequest.mockResolvedValue({ canSubmit: true, mentorName: 'Jane Doe' })

    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'POST',
      body: { token: REVIEW_TOKEN },
    })

    await handler(req, res)

    expect(mockRequest).toHaveBeenCalledWith('POST', '/api/v1/reviews/check', {
      body: { token: REVIEW_TOKEN },
    })
    const [, upstreamPath] = mockRequest.mock.calls[0]
    expect(upstreamPath).not.toContain(REVIEW_TOKEN)
    expect(res.statusCode).toBe(200)
    expect(res._getJSONData()).toEqual({ canSubmit: true, mentorName: 'Jane Doe' })
  })

  // Dual-read: links already sitting in mentees' inboxes keep working until the
  // cutover.
  it('still forwards a legacy request id to the legacy endpoint', async () => {
    mockRequest.mockResolvedValue({ canSubmit: true, mentorName: 'Jane Doe' })

    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'POST',
      body: { requestId: REQUEST_ID },
    })

    await handler(req, res)

    expect(mockRequest).toHaveBeenCalledWith('GET', `/api/v1/reviews/${REQUEST_ID}/check`)
    expect(res.statusCode).toBe(200)
  })

  it('prefers the token when both are present', async () => {
    mockRequest.mockResolvedValue({ canSubmit: true })

    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'POST',
      body: { token: REVIEW_TOKEN, requestId: REQUEST_ID },
    })

    await handler(req, res)

    expect(mockRequest).toHaveBeenCalledWith('POST', '/api/v1/reviews/check', {
      body: { token: REVIEW_TOKEN },
    })
  })

  // An expired or spent token is a 404 upstream; the page renders that message. A
  // blanket 500 turned every such link into "we could not verify".
  it('forwards an upstream 404 to the client', async () => {
    const { HttpError } = jest.requireActual('@/lib/go-api-client')
    mockRequest.mockRejectedValue(
      new HttpError(404, 'Not Found', JSON.stringify({ error: 'This review link is not valid' }))
    )

    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'POST',
      body: { token: REVIEW_TOKEN },
    })

    await handler(req, res)

    expect(res.statusCode).toBe(404)
    expect(res._getJSONData()).toEqual({ error: 'This review link is not valid' })
  })

  it('hides an upstream 5xx behind a generic 500', async () => {
    const { HttpError } = jest.requireActual('@/lib/go-api-client')
    mockRequest.mockRejectedValue(
      new HttpError(502, 'Bad Gateway', 'upstream connect error to 127.0.0.1:8080')
    )

    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'POST',
      body: { token: REVIEW_TOKEN },
    })

    await handler(req, res)

    expect(res.statusCode).toBe(500)
    expect(res._getJSONData()).toEqual({ error: 'Internal server error' })
  })
})
