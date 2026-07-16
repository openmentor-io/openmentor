import { createMocks } from 'node-mocks-http'
import type { NextApiRequest, NextApiResponse } from 'next'

// Mock the dependencies before importing handlers
jest.mock('@/lib/logger', () => ({
  info: jest.fn(),
  warn: jest.fn(),
  error: jest.fn(),
  logError: jest.fn(),
  logHttpRequest: jest.fn(),
}))

const mockConfirmMentorEmail = jest.fn()
const mockResendMentorConfirmation = jest.fn()

jest.mock('@/lib/go-api-client', () => {
  class HttpError extends Error {
    statusCode: number
    statusText: string
    body: string

    constructor(statusCode: number, statusText: string, body: string) {
      super(`Go API error: ${statusCode} ${statusText} - ${body}`)
      this.name = 'HttpError'
      this.statusCode = statusCode
      this.statusText = statusText
      this.body = body
    }
  }
  return {
    HttpError,
    getGoApiClient: () => ({
      confirmMentorEmail: mockConfirmMentorEmail,
      resendMentorConfirmation: mockResendMentorConfirmation,
    }),
  }
})

// Import handlers after mocks are set up
import confirmHandler from '@/pages/api/mentor/confirm'
import resendHandler from '@/pages/api/mentor/confirm-resend'
import { HttpError } from '@/lib/go-api-client'

describe('api/mentor/confirm', () => {
  beforeEach(() => {
    jest.clearAllMocks()
  })

  it('returns 405 for non-POST requests', async () => {
    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({ method: 'GET' })

    await confirmHandler(req, res)

    expect(res.statusCode).toBe(405)
    expect(res._getJSONData()).toEqual({ error: 'Method not allowed' })
  })

  it('forwards the token to the Go API and returns its response', async () => {
    const body = { token: 'mcf_abc123' }
    mockConfirmMentorEmail.mockResolvedValue({ success: true })

    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({ method: 'POST', body })

    await confirmHandler(req, res)

    expect(mockConfirmMentorEmail).toHaveBeenCalledWith(body)
    expect(res.statusCode).toBe(200)
    expect(res._getJSONData()).toEqual({ success: true })
  })

  it('passes through already-confirmed responses', async () => {
    mockConfirmMentorEmail.mockResolvedValue({ success: true, already: true })

    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'POST',
      body: { token: 'mcf_abc123' },
    })

    await confirmHandler(req, res)

    expect(res.statusCode).toBe(200)
    expect(res._getJSONData()).toEqual({ success: true, already: true })
  })

  it('passes through the 410 expired status and error code', async () => {
    mockConfirmMentorEmail.mockRejectedValue(
      new HttpError(410, 'Gone', JSON.stringify({ success: false, code: 'token_expired' }))
    )

    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'POST',
      body: { token: 'mcf_expired' },
    })

    await confirmHandler(req, res)

    expect(res.statusCode).toBe(410)
    expect(res._getJSONData()).toEqual({ success: false, code: 'token_expired' })
  })

  it('passes through the 400 invalid-token status', async () => {
    mockConfirmMentorEmail.mockRejectedValue(
      new HttpError(400, 'Bad Request', JSON.stringify({ success: false, code: 'invalid_token' }))
    )

    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'POST',
      body: { token: 'nope' },
    })

    await confirmHandler(req, res)

    expect(res.statusCode).toBe(400)
    expect(res._getJSONData()).toEqual({ success: false, code: 'invalid_token' })
  })

  it('returns 500 for unexpected errors', async () => {
    mockConfirmMentorEmail.mockRejectedValue(new Error('connection refused'))

    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'POST',
      body: { token: 'mcf_abc123' },
    })

    await confirmHandler(req, res)

    expect(res.statusCode).toBe(500)
    expect(res._getJSONData()).toEqual({ error: 'Internal server error' })
  })
})

describe('api/mentor/confirm-resend', () => {
  beforeEach(() => {
    jest.clearAllMocks()
  })

  it('returns 405 for non-POST requests', async () => {
    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({ method: 'GET' })

    await resendHandler(req, res)

    expect(res.statusCode).toBe(405)
  })

  it('forwards the token to the Go API resend endpoint', async () => {
    const body = { token: 'mcf_expired' }
    mockResendMentorConfirmation.mockResolvedValue({ success: true })

    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({ method: 'POST', body })

    await resendHandler(req, res)

    expect(mockResendMentorConfirmation).toHaveBeenCalledWith(body)
    expect(res.statusCode).toBe(200)
    expect(res._getJSONData()).toEqual({ success: true })
  })

  it('passes through the 400 invalid-token status', async () => {
    mockResendMentorConfirmation.mockRejectedValue(
      new HttpError(400, 'Bad Request', JSON.stringify({ success: false, code: 'invalid_token' }))
    )

    const { req, res } = createMocks<NextApiRequest, NextApiResponse>({
      method: 'POST',
      body: { token: 'nope' },
    })

    await resendHandler(req, res)

    expect(res.statusCode).toBe(400)
    expect(res._getJSONData()).toEqual({ success: false, code: 'invalid_token' })
  })
})
